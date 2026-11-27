package supply

import "testing"

func TestCompareSortsByTotalThenLeadTimeThenMOQ(t *testing.T) {
	quotes := []Quote{
		{ID: 1, SupplierID: 1, SupplierName: "甲", PartName: "外壳", UnitPrice: 12.5, Currency: "CNY", MOQ: 100, LeadTimeDays: 20,
			Tiers: []Tier{{MinQty: 500, UnitPrice: 11.0}, {MinQty: 1000, UnitPrice: 10.0}}},
		{ID: 2, SupplierID: 2, SupplierName: "乙", PartName: "外壳", UnitPrice: 10.5, Currency: "CNY", MOQ: 200, LeadTimeDays: 7},
		{ID: 3, SupplierID: 3, SupplierName: "丙", PartName: "外壳", UnitPrice: 10.0, Currency: "CNY", MOQ: 50, LeadTimeDays: 30},
	}
	got, err := Compare(quotes, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("结果数 = %d", len(got))
	}
	// 甲命中 1000 档 = 10.0*1000=10000；乙 = 10.5*1000=10500；丙 = 10.0*1000=10000
	// 甲与丙同价，甲交期 20 < 丙 30 → 甲在前。
	if got[0].QuoteID != 1 || got[1].QuoteID != 3 || got[2].QuoteID != 2 {
		t.Fatalf("排序错误：%+v", got)
	}
	if !got[0].Recommended || got[0].Reason == "" {
		t.Fatalf("第一条应为推荐且有理由：%+v", got[0])
	}
	if got[0].MatchedTier == nil || *got[0].MatchedTier != 1 {
		t.Fatalf("甲应命中第二档：%+v", got[0])
	}
}

func TestCompareUsesIntegerCents(t *testing.T) {
	// 0.1 * 3 与 0.3：浮点会出 0.30000000000000004，整数分应判同价。
	quotes := []Quote{
		{ID: 1, SupplierID: 1, PartName: "x", UnitPrice: 0.1, Currency: "CNY", MOQ: 1},
		{ID: 2, SupplierID: 2, PartName: "x", UnitPrice: 0.3, Currency: "CNY", MOQ: 3},
	}
	got, err := Compare(quotes, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TotalCents != 30 || got[1].TotalCents != 90 {
		t.Fatalf("整数分错误：%+v", got)
	}
}

func TestCompareRespectsMOQ(t *testing.T) {
	q := Quote{ID: 1, SupplierID: 1, PartName: "x", UnitPrice: 10, Currency: "CNY", MOQ: 500}
	got, err := Compare([]Quote{q}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TotalCents != 500000 {
		t.Fatalf("低于 MOQ 应按 MOQ 算：%+v", got[0])
	}
}

func TestCompareRejectsInvalid(t *testing.T) {
	cases := []Quote{
		{ID: 1, UnitPrice: 0, Currency: "CNY", MOQ: 1},
		{ID: 2, UnitPrice: 1, Currency: "CNY", MOQ: 0},
		{ID: 3, UnitPrice: 1, Currency: "EUR", MOQ: 1},
		{ID: 4, UnitPrice: 1, Currency: "CNY", MOQ: 1, LeadTimeDays: -1},
	}
	for _, q := range cases {
		if _, err := Compare([]Quote{q}, 100); err == nil {
			t.Fatalf("应拒绝非法报价：%+v", q)
		}
	}
	if _, err := Compare(nil, 0); err == nil {
		t.Fatal("qty=0 应拒绝")
	}
}

func TestValidateTiers(t *testing.T) {
	q := Quote{UnitPrice: 1, Currency: "CNY", MOQ: 1, Tiers: []Tier{{MinQty: 0, UnitPrice: 1}}}
	if err := q.Validate(); err == nil {
		t.Fatal("tier min_qty=0 应拒绝")
	}
}
