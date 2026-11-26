-- 0001_init.sql — radar 自有库的初始结构
--
-- 四张表全部属于 radar，与 RFERP 的数据库没有任何关系。
-- 删掉这个文件所在的库，只会丢选品历史，不影响 RFERP 里已建好的产品与 BOM。

CREATE TABLE IF NOT EXISTS opportunity (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword           TEXT    NOT NULL,
    name              TEXT    NOT NULL,
    one_line          TEXT    NOT NULL DEFAULT '',
    category          TEXT    NOT NULL DEFAULT '',
    score_demand      REAL    NOT NULL DEFAULT 0,
    score_pain        REAL    NOT NULL DEFAULT 0,
    score_trend       REAL    NOT NULL DEFAULT 0,
    score_competition REAL    NOT NULL DEFAULT 0,
    score_cost        REAL,
    score_overall     REAL    NOT NULL DEFAULT 0,
    confidence        REAL    NOT NULL DEFAULT 0,
    verdict           TEXT    NOT NULL DEFAULT '',
    reasons_json      TEXT    NOT NULL DEFAULT '[]',
    pain_points_json  TEXT    NOT NULL DEFAULT '[]',
    status            TEXT    NOT NULL DEFAULT 'analyzed',
    created_at        TEXT    NOT NULL,
    updated_at        TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_opportunity_created ON opportunity(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_opportunity_status ON opportunity(status);

CREATE TABLE IF NOT EXISTS opportunity_evidence (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    opportunity_id INTEGER NOT NULL REFERENCES opportunity(id) ON DELETE CASCADE,
    source         TEXT    NOT NULL,
    title          TEXT    NOT NULL,
    snippet        TEXT    NOT NULL DEFAULT '',
    url            TEXT    NOT NULL,
    votes          INTEGER NOT NULL DEFAULT 0,
    comments       INTEGER NOT NULL DEFAULT 0,
    authority      REAL    NOT NULL DEFAULT 0,
    published_at   TEXT,
    rank_no        INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_evidence_opportunity ON opportunity_evidence(opportunity_id, rank_no);

CREATE TABLE IF NOT EXISTS cost_model (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    opportunity_id  INTEGER NOT NULL REFERENCES opportunity(id) ON DELETE CASCADE,
    bom_json        TEXT    NOT NULL,
    total_cost      REAL    NOT NULL DEFAULT 0,
    suggested_price REAL    NOT NULL DEFAULT 0,
    margin          REAL    NOT NULL DEFAULT 0,
    price_source    TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cost_opportunity ON cost_model(opportunity_id, created_at DESC);

CREATE TABLE IF NOT EXISTS adoption (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    opportunity_id   INTEGER NOT NULL REFERENCES opportunity(id) ON DELETE CASCADE,
    reef_base        TEXT    NOT NULL,
    reef_database_id TEXT    NOT NULL DEFAULT '',
    product_id       INTEGER NOT NULL DEFAULT 0,
    bom_created      INTEGER NOT NULL DEFAULT 0,
    bom_skipped      INTEGER NOT NULL DEFAULT 0,
    status           TEXT    NOT NULL,
    message          TEXT    NOT NULL DEFAULT '',
    operator         TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL
);

-- 一个候选最多一条成功记录。部分索引让"重复采纳"在数据库层就挡住，
-- 而不是只靠服务层判断（并发下服务层判断会漏）。
CREATE UNIQUE INDEX IF NOT EXISTS uk_adoption_ok
    ON adoption(opportunity_id) WHERE status = 'ok';

CREATE INDEX IF NOT EXISTS idx_adoption_opportunity ON adoption(opportunity_id, created_at DESC);
