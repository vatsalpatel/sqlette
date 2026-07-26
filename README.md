# sqlette

A small SQLite-like database written from scratch in Go — lexer, parser, planner, executor, B+tree, pager, and a rollback journal. No dependencies outside the standard library.

This is a learning project: the goal is to understand query planning, execution, and storage by building them, not to be a drop-in SQLite replacement. The on-disk format borrows SQLite's ideas (fixed-size pages, slotted cells, varint records, rowid-keyed B+trees) but is not byte-compatible with it.

## Status

Working today:

- `CREATE TABLE`, `INSERT ... VALUES` (multi-row), `SELECT ... FROM ... WHERE`, `EXPLAIN`
- `BEGIN` / `COMMIT` / `ROLLBACK`, plus autocommit for bare statements
- Expressions: comparison, `AND`/`OR`/`NOT`, `IS`, arithmetic, `||`, unary minus, column refs and aliases
- Five storage classes — `NULL`, `INTEGER`, `REAL`, `TEXT`, `BLOB` — with SQLite-style dynamic typing and three-valued `NULL` logic
- Data persists across restarts, and a crash mid-transaction rolls back on the next open

Not implemented yet: `UPDATE`, `DELETE`, indexes, `ORDER BY`, `LIMIT`, joins, aggregates, subqueries. See [Roadmap](#roadmap).

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
| `internal/catalog` | Schema registry — tables, columns, root pages — marshalled into the database file. |
| `internal/plan` | Logical plan nodes: `SeqScan`, `Filter`, `Project`. What `EXPLAIN` prints. |
| `internal/exec` | Builds and runs the iterator tree; contains the expression evaluator. |
| `internal/record` | Row ⇄ bytes: a varint header of serial types followed by the body. |
| `internal/btree` | B+tree over pages — search, insert, splits, cursors. Slotted pages, rowid-keyed. |
| `internal/pager` | Page number → 4 KB page. Cache, dirty tracking, allocation, fsync. |
| `internal/journal` | Rollback journal: original pages copied aside, replayed on crash-open. |
| `internal/storage` | Tables over btrees, plus the `Cursor` interface the executor scans through. |
| `internal/engine` | The façade: `Open(path)`, `Exec(stmt)`, transaction control. The only package `cmd/` imports. |

The dependency graph is kept acyclic and the parser never touches the catalog — name resolution happens later, against the schema.

## On-disk format

One file, 4 KB pages, 1-based page numbers. Page 1 holds the serialized catalog; every table is a separate B+tree keyed by rowid.

A page in a table tree is a slotted page:

```
header (10 bytes)
  [0]     type          uint8   leaf=1, interior=2
  [2:4]   cellCount     uint16
  [4:6]   contentStart  uint16  start of the back-packed cell area
  [6:10]  extra         uint32  leaf: right-sibling page; interior: leftmost child
pointer array
  [10 : 10+2*cellCount] uint16 cell offsets, sorted by key
cells (packed downward from the end of the page)
  leaf cell:     rowid int64 | payloadLen uvarint | payload
  interior cell: separator int64 | child uint32
```

Row payloads are `record`-encoded: a uvarint field count, then per field a uvarint serial type (`0`=NULL, `1`=INTEGER, `2`=REAL, `3`=TEXT, `4`=BLOB) and its big-endian body.

## Transactions

Atomicity and durability come from a rollback journal, the approach SQLite shipped before WAL. Before a page is modified its original content is copied to `<db>-journal`; commit writes the pages, fsyncs, and deletes the journal. The journal's *existence* is the uncommitted flag — there is no commit record. If it is present when the database is opened, the original pages are copied back before anything else happens, so a `kill -9` mid-transaction reopens as though the transaction never started.

Statements outside an explicit `BEGIN` run in their own transaction, and a rollback rebuilds the in-memory catalog from the restored pages rather than trying to undo it in place.

## Testing

```
go test ./...
```

Per-package unit tests throughout, including B+tree splits exercised with deliberately tiny pages, `NULL` truth-table tests on the value system, record round-trips, journal recovery at each fsync point, and end-to-end tests that pipe SQL through the REPL.

## Roadmap

Built so far: front end → in-memory engine → record/pager/btree storage → transactions. Next up:

- **Mutations** — `UPDATE` / `DELETE`, which force B+tree delete with page-space reclamation and the collect-then-mutate discipline
- **Indexes** — `CREATE INDEX`, index btrees maintained by every write, and a planner that picks `IndexScan` over `SeqScan`
- **Fuller SQL** — `ORDER BY`, `LIMIT`, nested-loop joins, `GROUP BY` and aggregates
- **Hardening** — page-cache eviction, overflow pages, freelist reuse, parser and codec fuzzing, differential testing against real SQLite
- **Endgame** — run SQLite's own `sqllogictest` corpus and grind the pass rate upward

## References

Worth reading if you're building something similar:

- SQLite's [architecture](https://sqlite.org/arch.html), [file format](https://sqlite.org/fileformat2.html), and [query planner](https://sqlite.org/queryplanner.html) docs
- [Let's Build a Simple Database](https://cstack.github.io/db_tutorial/) — C, pager and btree focused
- [Build Your Own Database From Scratch in Go](https://build-your-own.org/database/)
- [CMU 15-445](https://15445.courses.cs.cmu.edu/) — the best planner and execution coverage anywhere
- *Database Internals*, Alex Petrov — storage-engine depth

## License

Apache-2.0. See [LICENSE](LICENSE).
