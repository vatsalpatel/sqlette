CREATE TABLE t (a INT, b TEXT, c INT);
INSERT INTO t VALUES (5, 'x', 1), (5, 'y', 2), (5, 'x', 3), (1, 'x', 4), (9, 'y', 5);
CREATE INDEX idx_a ON t (a);
SELECT * FROM t WHERE a = 5 AND b = 'x';
SELECT * FROM t WHERE a = 5 AND b = 'x' AND c = 3;
SELECT * FROM t WHERE a > 1 AND b = 'y';
SELECT * FROM t WHERE a = 5 OR b = 'y';
SELECT * FROM t WHERE a <> 5;
