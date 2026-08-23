# sqlette

A small SQLite-like database written from scratch in Go: lexer, parser, planner, executor, B+tree, pager, and a rollback journal. The engine itself uses nothing outside the standard library; the only dependency in `go.mod` is real SQLite, used by the tests to diff results against.

This is a learning project: the goal is to understand query planning, execution, and storage by building them, not to be a drop-in SQLite replacement. The on-disk format borrows SQLite's ideas (fixed-size pages, slotted cells, varint records, rowid-keyed B+trees) but is not byte-compatible with it.

## Status

Working today:

- `CREATE TABLE`, `INSERT ... VALUES` (multi-row), `SELECT ... FROM ... WHERE`, `UPDATE`, `DELETE`, `EXPLAIN`
- `CREATE INDEX` and `CREATE UNIQUE INDEX`, maintained by every insert, update and delete
- A planner that picks an index scan over a table scan when the `WHERE` clause allows it, and shows you which it chose
- `BEGIN` / `COMMIT` / `ROLLBACK`, plus autocommit for bare statements
- Expressions: comparison, `AND`/`OR`/`NOT`, `IS`, arithmetic, `||`, unary minus, column refs and aliases
- Five storage classes (`NULL`, `INTEGER`, `REAL`, `TEXT`, `BLOB`) with SQLite-style dynamic typing and three-valued `NULL` logic
- Data persists across restarts, and a crash mid-transaction rolls back on the next open

A point lookup on an indexed column of a 100,000-row table touches 6 pages. The same lookup as a table scan touches 1,267.

Not implemented yet: `ORDER BY`, `LIMIT`, joins, aggregates, `GROUP BY`, subqueries, `DROP`. See [Roadmap](#roadmap).

## Quick start

```
go build -o sqlette ./cmd/sqlette
./sqlette
```

The shell opens `sqlette.db` in the current directory, creating it if absent. Statements are read until a `;`; `.exit`, `.quit`, and `.q` leave the shell.

```
sqlette> create table users (id int, name text);
ok
sqlette> insert into users values (1, 'ada'), (2, 'alan'), (3, 'grace');
3 rows inserted
sqlette> select id, name from users where id > 1;
id | name
2 | 'alan'
3 | 'grace'
sqlette> explain select * from users where id > 1;
(project *)
  (filter (> id 1))
    (seqscan users)
sqlette> create index idx_name on users (name);
ok
sqlette> explain select * from users where name = 'ada';
(project *)
  (indexscan users using idx_name (= name 'ada'))
sqlette> begin;
ok
sqlette> insert into users values (4, 'linus');
1 rows inserted
sqlette> rollback;
ok
sqlette> select * from users;
id | name
1 | 'ada'
2 | 'alan'
3 | 'grace'
```

It reads from a pipe just as happily, which is how the end-to-end tests drive it:

```
printf "create table t (a int);\ninsert into t values (1);\nselect * from t;\n" | ./sqlette
```

## Architecture

```
SQL text → lexer → parser → plan → exec → storage → btree → pager → journal → disk
           ──── front end ────   ── execution ──   ────────── storage ──────────
```

Execution is a Volcano-style iterator tree: each operator exposes `Open`/`Next`/`Close` and pulls rows from its input, so a scan never materializes a whole table.

| Package | Responsibility |
|---|---|
| `internal/token`, `internal/lexer` | SQL text → tokens. Case-insensitive keywords, `'strings'`, `"identifiers"`, numbers, `--` comments. |
| `internal/ast` | Statement and expression nodes. Pure data, plus a pretty-printer. |
| `internal/parser` | Recursive descent for statements, Pratt / precedence climbing for expressions. |
| `internal/values` | The value system: five storage classes, comparison and arithmetic rules, three-valued `NULL` logic. |
| `internal/catalog` | Schema registry: tables, columns, indexes and their root pages, marshalled into the database file. |
| `internal/plan` | Plan nodes: `SeqScan`, `IndexScan`, `Filter`, `Project`, `Delete`, `Update`. What `EXPLAIN` prints. |
| `internal/planner` | Access-path selection. Reads the `WHERE` clause, picks an index or a scan, and leaves whatever the scan cannot enforce in a filter above it. |
| `internal/exec` | Builds and runs the iterator tree; contains the expression evaluator. |
| `internal/record` | Row ⇄ bytes: a varint header of serial types followed by the body. |
| `internal/btree` | B+tree over pages: search, insert, delete, splits, cursors, seeks. Slotted pages, byte-string keys ordered by an injected comparator. |
| `internal/pager` | Page number → 4 KB page. Cache, dirty tracking, allocation, fsync. |
| `internal/journal` | Rollback journal: original pages copied aside, replayed on crash-open. |
| `internal/storage` | Tables and indexes over btrees, index key encoding, and the `Cursor` interface the executor scans through. |
| `internal/engine` | The façade: `Open(path)`, `Exec(stmt)`, transaction control. The only package `cmd/` imports. |

The dependency graph is kept acyclic and the parser never touches the catalog: name resolution happens later, against the schema. `btree` knows nothing about SQL values either. It orders keys through a comparison function that `storage` hands it, which is what lets one B+tree implementation serve both rowid-keyed tables and index trees keyed by column values.

## On-disk format

One file, 4 KB pages, 1-based page numbers. Page 1 holds the serialized catalog. Every table is a B+tree keyed by rowid, and every index is a B+tree keyed by its column values.

Every page in either kind of tree is a slotted page:

```
header (10 bytes)
  [0]     type          uint8   table leaf=1, table interior=2,
  //                              index leaf=3, index interior=4
  [2:4]   cellCount     uint16
  [4:6]   contentStart  uint16  start of the back-packed cell area
  [6:10]  extra         uint32  leaf: right-sibling page; interior: leftmost child
pointer array
  [10 : 10+2*cellCount] uint16 cell offsets, sorted by key
cells (packed downward from the end of the page)
  table leaf:      rowid int64      | payloadLen uvarint | payload
  table interior:  separator int64  | child uint32
  index leaf:      keyLen uvarint | key | payloadLen uvarint (always 0)
  index interior:  keyLen uvarint | key | child uint32
```

A table key is always the 8 bytes of a rowid, so it needs no length. An index key is a tuple of arbitrary values, so it carries one. The page's own type byte says which layout its cells use, which is what lets both kinds of tree live in the same file with their pages interleaved.

An index key is a `record`-encoded tuple of the indexed columns followed by the row's rowid. That suffix is doing real work: it makes every key unique even in a non-unique index, so removing one entry is an exact-key delete rather than a scan for the matching row. Keys are compared field by field with the same value rules the rest of the engine uses, so `NULL` sorts before numbers, which sort before text, which sort before blobs.

Row payloads are `record`-encoded: a uvarint field count, then per field a uvarint serial type (`0`=NULL, `1`=INTEGER, `2`=REAL, `3`=TEXT, `4`=BLOB) and its big-endian body.

## Transactions

Atomicity and durability come from a rollback journal, the approach SQLite shipped before WAL. Before a page is modified its original content is copied to `<db>-journal`; commit writes the pages, fsyncs, and deletes the journal. The journal's *existence* is the uncommitted flag — there is no commit record. If it is present when the database is opened, the original pages are copied back before anything else happens, so a `kill -9` mid-transaction reopens as though the transaction never started.

Statements outside an explicit `BEGIN` run in their own transaction, and a rollback rebuilds the in-memory catalog from the restored pages rather than trying to undo it in place.

## Indexes

`CREATE INDEX idx ON t (col)` builds a second B+tree and scans the table to fill it. From then on every `INSERT`, `UPDATE` and `DELETE` maintains it, which is why that maintenance lives behind the storage layer's verbs rather than in the engine: a caller cannot write a row without the indexes moving with it.

`UPDATE` and `DELETE` read the row before they change it. The index entry to remove is built from the row's *old* column values, and neither statement is handed those. Skipping that read leaves an index entry pointing at a row that no longer has that value, which never errors and simply makes later queries return the wrong rows.

The planner splits the `WHERE` clause on `AND`, looks for `column op literal` on some index's leading column, and turns the best one into a range to scan. Whatever the scan cannot enforce stays in a filter above it. The rule it obeys throughout: leaving a predicate in the filter is always safe, dropping one the scan does not enforce never is.

`EXPLAIN` prints the plan that actually runs, because the executor and `EXPLAIN` build it through the same function.

Indexes cost a second lookup. An entry holds only the key, so finding a row means reading the rowid off that key and searching the table tree for it. There are no covering indexes yet.

## Testing

```
go test ./...
```

Per-package unit tests throughout, including B+tree splits exercised with deliberately fat payloads, `NULL` truth-table tests on the value system, record round-trips, journal recovery at each fsync point, and end-to-end tests that pipe SQL through the REPL.

Three kinds of test carry more weight than the rest:

- **Differential testing against real SQLite.** Golden scripts in `internal/engine/testdata/diff` run through both engines and every result set is compared. Both sides are rendered from typed values rather than from printed output, so a float-formatting difference can never masquerade as a wrong answer.
- **Index consistency.** After a stream of random mutations, each index is rebuilt from a full table scan and compared against what is actually stored. A missed maintenance path produces no error at all, so a recomputation is the only honest check.
- **Page counts, not wall clock.** `pager.Reads` counts page accesses, which makes "the index is faster" an exact assertion (6 pages against 1,267) instead of a timing threshold that flakes in CI.

One test file opens `internal/engine/testdata/pre-m6.db`, a database written before the B+tree learned about byte-string keys, to prove the on-disk format for tables did not move.

## Roadmap

Built so far: front end, in-memory engine, record/pager/btree storage, transactions, mutations, indexes and a planner that uses them. Next up:

- **Fuller SQL** — `ORDER BY`, `LIMIT`, nested-loop joins, `GROUP BY` and aggregates. Sorting and grouping bring the first operators that cannot stream, and joins are where an index starts serving the inner side of a loop.
- **Hardening** — page-cache eviction, overflow pages for large rows, freelist reuse so `DROP` can give pages back, merging underfull B+tree leaves, statement-level savepoints, and parser and codec fuzzing
- **Endgame** — run SQLite's own `sqllogictest` corpus and grind the pass rate upward

Known simplifications, all deliberate: B+tree leaves are never merged after a delete, so a heavily-deleted table stays sparse. A failed statement inside an explicit transaction is not rolled back on its own, because there are no savepoints yet. The catalog lives in a single 4 KB page, which caps how many tables and indexes a database can hold. A row must fit in one page.

## References

Worth reading if you're building something similar:

- SQLite's [architecture](https://sqlite.org/arch.html), [file format](https://sqlite.org/fileformat2.html), and [query planner](https://sqlite.org/queryplanner.html) docs
- [Let's Build a Simple Database](https://cstack.github.io/db_tutorial/) — C, pager and btree focused
- [Build Your Own Database From Scratch in Go](https://build-your-own.org/database/)
- [CMU 15-445](https://15445.courses.cs.cmu.edu/) — the best planner and execution coverage anywhere
- *Database Internals*, Alex Petrov — storage-engine depth

## License

Apache-2.0. See [LICENSE](LICENSE).
