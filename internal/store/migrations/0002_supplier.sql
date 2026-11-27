-- 0002_supplier.sql — 供应链比价数据（issue #4）
--
-- 金额口径：用 REAL 存，与 internal/domain 的成本模型一致。
-- 比价排序时把金额转成整数分再比较，避免浮点误差导致"看着便宜的更贵"。

CREATE TABLE IF NOT EXISTS supplier (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    region         TEXT    NOT NULL DEFAULT '',
    contact        TEXT    NOT NULL DEFAULT '',
    payment_terms  TEXT    NOT NULL DEFAULT '',
    note           TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS supplier_quote (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    opportunity_id  INTEGER NOT NULL REFERENCES opportunity(id) ON DELETE CASCADE,
    supplier_id     INTEGER NOT NULL REFERENCES supplier(id) ON DELETE RESTRICT,
    part_name       TEXT    NOT NULL,
    unit_price      REAL    NOT NULL,
    currency        TEXT    NOT NULL DEFAULT 'CNY',
    moq             INTEGER NOT NULL DEFAULT 1,
    lead_time_days  INTEGER NOT NULL DEFAULT 0,
    tier_json       TEXT    NOT NULL DEFAULT '[]',
    selected        INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_quote_opportunity ON supplier_quote(opportunity_id);

-- 一个机会最多一条 selected=1 的报价。部分唯一索引在数据库层挡住并发重复选定。
CREATE UNIQUE INDEX IF NOT EXISTS uk_quote_selected
    ON supplier_quote(opportunity_id) WHERE selected = 1;
