-- 0003_cost_extras.sql — 成本附加项（issue #5 验收：包装 + 头程运费 + 佣金）
-- 幂等：SQLite 不支持 ADD COLUMN IF NOT EXISTS，用迁移版本号保证只执行一次。

ALTER TABLE cost_model ADD COLUMN packaging REAL NOT NULL DEFAULT 0;
ALTER TABLE cost_model ADD COLUMN freight REAL NOT NULL DEFAULT 0;
ALTER TABLE cost_model ADD COLUMN commission_rate REAL NOT NULL DEFAULT 0;
