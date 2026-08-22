CREATE TABLE t (a INT, b TEXT);
INSERT INTO t VALUES (5, 'e1'), (1, 'a'), (5, 'e2'), (9, 'i'), (5, 'e3');
CREATE INDEX idx_a ON t (a);
SELECT * FROM t WHERE a = 5;
SELECT * FROM t WHERE a = 1;
SELECT * FROM t WHERE a = 4;
SELECT b FROM t WHERE a = 5;
