CREATE TABLE t (a INT, b TEXT);
INSERT INTO t VALUES (1, 'one'), (2.5, 'two-point-five'), (10, 'ten');
CREATE INDEX idx_a ON t (a);
SELECT * FROM t WHERE a > 1;
SELECT * FROM t WHERE a >= 2.5;
SELECT * FROM t WHERE a = 2.5;
