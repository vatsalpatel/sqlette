CREATE TABLE t (a INT, b TEXT);
INSERT INTO t VALUES (5, 'e'), (1, 'a'), (9, 'i'), (3, 'c'), (7, 'g');
SELECT * FROM t;
SELECT a FROM t WHERE a > 3;
SELECT b FROM t WHERE a = 5;
