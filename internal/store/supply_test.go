package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rf-us/zhihai-radar/internal/domain"
	"github.com/rf-us/zhihai-radar/internal/supply"
)

func seedOpportunity(t *testing.T, st *Store) int64 {
	t.Helper()
	o := &Opportunity{Keyword: "便携咖啡机", Name: "便携咖啡机", Scores: domain.Scores{Demand: 80, Pain: 70}}
	if err := st.CreateOpportunity(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func TestMigration0002IsIdempotent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("重复迁移失败：%v", err)
	}
}

func TestSupplierQuotesCRUDAndSelect(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	oid := seedOpportunity(t, st)

	s1, err := st.CreateSupplier(ctx, supply.Supplier{Name: "甲", Region: "深圳"})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := st.CreateSupplier(ctx, supply.Supplier{Name: "乙", Region: "东莞"})
	if err != nil {
		t.Fatal(err)
	}

	q1, err := st.CreateQuote(ctx, supply.Quote{
		OpportunityID: oid, SupplierID: s1.ID, PartName: "外壳", UnitPrice: 12.5, Currency: "CNY", MOQ: 100, LeadTimeDays: 20,
		Tiers: []supply.Tier{{MinQty: 500, UnitPrice: 11}, {MinQty: 1000, UnitPrice: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	q2, err := st.CreateQuote(ctx, supply.Quote{
		OpportunityID: oid, SupplierID: s2.ID, PartName: "外壳", UnitPrice: 11, Currency: "CNY", MOQ: 200, LeadTimeDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}

	quotes, err := st.ListQuotes(ctx, oid)
	if err != nil || len(quotes) != 2 {
		t.Fatalf("ListQuotes = %d, err = %v", len(quotes), err)
	}
	if quotes[0].SupplierName != "甲" || len(quotes[0].Tiers) != 2 {
		t.Fatalf("报价回读错误：%+v", quotes[0])
	}

	if err := st.SelectQuote(ctx, oid, q1.ID); err != nil {
		t.Fatal(err)
	}
	// 再选 q2：q1 应自动取消。
	if err := st.SelectQuote(ctx, oid, q2.ID); err != nil {
		t.Fatal(err)
	}
	quotes, _ = st.ListQuotes(ctx, oid)
	selected := 0
	for _, q := range quotes {
		if q.Selected {
			selected++
			if q.ID != q2.ID {
				t.Fatalf("选中的是 %d，想要 %d", q.ID, q2.ID)
			}
		}
	}
	if selected != 1 {
		t.Fatalf("selected 数量 = %d，想要 1", selected)
	}

	// 有报价引用时删除供应商应报 ErrQuoted，并列出引用它的机会。
	err = st.DeleteSupplier(ctx, s2.ID)
	var quoted *ErrQuoted
	if !errors.As(err, &quoted) || len(quoted.OpportunityIDs) != 1 || quoted.OpportunityIDs[0] != oid {
		t.Fatalf("应报带机会 ID 的 ErrQuoted，got %v", err)
	}
}

func TestCreateQuoteRejectsInvalid(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	oid := seedOpportunity(t, st)
	s, _ := st.CreateSupplier(ctx, supply.Supplier{Name: "甲"})

	_, err := st.CreateQuote(ctx, supply.Quote{
		OpportunityID: oid, SupplierID: s.ID, PartName: "x", UnitPrice: 0, Currency: "CNY", MOQ: 1,
	})
	if err == nil {
		t.Fatal("单价为 0 应拒绝")
	}
}

func TestSelectQuoteRejectsForeignOpportunity(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	oid1 := seedOpportunity(t, st)
	oid2 := seedOpportunity(t, st)
	s, _ := st.CreateSupplier(ctx, supply.Supplier{Name: "甲"})
	q, _ := st.CreateQuote(ctx, supply.Quote{
		OpportunityID: oid1, SupplierID: s.ID, PartName: "x", UnitPrice: 10, Currency: "CNY", MOQ: 1,
	})
	if err := st.SelectQuote(ctx, oid2, q.ID); err == nil {
		t.Fatal("跨机会选定应拒绝")
	}
}

func TestConcurrentSelectKeepsOneSelected(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	oid := seedOpportunity(t, st)
	s1, _ := st.CreateSupplier(ctx, supply.Supplier{Name: "甲"})
	s2, _ := st.CreateSupplier(ctx, supply.Supplier{Name: "乙"})
	q1, _ := st.CreateQuote(ctx, supply.Quote{OpportunityID: oid, SupplierID: s1.ID, PartName: "x", UnitPrice: 10, Currency: "CNY", MOQ: 1})
	q2, _ := st.CreateQuote(ctx, supply.Quote{OpportunityID: oid, SupplierID: s2.ID, PartName: "x", UnitPrice: 11, Currency: "CNY", MOQ: 1})

	// 两个 goroutine 同时选不同报价；Store 单连接串行化，部分唯一索引兜底。
	var wg sync.WaitGroup
	for _, qid := range []int64{q1.ID, q2.ID} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_ = st.SelectQuote(ctx, oid, id) // 并发下允许一方失败
		}(qid)
	}
	wg.Wait()

	quotes, err := st.ListQuotes(ctx, oid)
	if err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, q := range quotes {
		if q.Selected {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("并发选定后应只有一条 selected=1，实际 %d", selected)
	}
}
