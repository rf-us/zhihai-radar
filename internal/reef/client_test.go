package reef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeReef 尽量照抄 RFERP 的行为，尤其是那些会咬人的细节：
//   - POST /api/products 用 DisallowUnknownFields
//   - 零行列表返回 null
//   - products.code 允许重复
type fakeReef struct {
	mu       sync.Mutex
	products []Product
	boms     map[int64][]BOMItem
	nextID   int64

	failBOM    bool
	slowCreate time.Duration
	parts      map[int64]Part
	partKept   bool // 删除零件时返回 409（模拟被引用）
}

func newFakeReef() *fakeReef {
	return &fakeReef{boms: map[int64][]BOMItem{}, parts: map[int64]Part{}, nextID: 1}
}

func (f *fakeReef) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/v1/serverinfo":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"appVersion":"2.3.0","apiVersion":1,"schemaVersion":13,"databaseId":"db-test","storage":"sqlite"}`)

	case r.URL.Path == "/api/login" && r.Method == http.MethodPost:
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Username != "admin" || body.Password != "pw" {
			writeFakeErr(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		fmt.Fprint(w, `{"token":"t0","user":{"id":1,"username":"admin","role":"admin","status":1}}`)

	case r.URL.Path == "/api/products" && r.Method == http.MethodGet:
		f.mu.Lock()
		list := make([]Product, len(f.products))
		copy(list, f.products)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if len(list) == 0 {
			fmt.Fprint(w, "null")
			return
		}
		_ = json.NewEncoder(w).Encode(list)

	case r.URL.Path == "/api/parts" && r.Method == http.MethodPost:
		var in CreatePartInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeFakeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		f.mu.Lock()
		id := f.nextID
		f.nextID++
		p := Part{ID: id, Code: in.Code, Name: in.Name, Unit: in.Unit, Supplier: in.Supplier}
		f.parts[id] = p
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(p)

	case r.URL.Path == "/api/parts" && r.Method == http.MethodDelete:
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		f.mu.Lock()
		_, exists := f.parts[id]
		if exists && !f.partKept {
			delete(f.parts, id)
		}
		f.mu.Unlock()
		if exists && f.partKept {
			writeFakeErr(w, http.StatusConflict, "零件已被引用")
			return
		}
		fmt.Fprint(w, `{"status":"ok"}`)

	case r.URL.Path == "/api/products" && r.Method == http.MethodPost:
		// 与 RFERP 的 decodeProductWrite 一致：未知字段直接 400。
		var in struct {
			Code   string  `json:"code"`
			Name   string  `json:"name"`
			Spec   *string `json:"spec"`
			Unit   string  `json:"unit"`
			Status *int    `json:"status"`
			ID     int64   `json:"id"`
			Ver    int     `json:"version"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeFakeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		f.mu.Lock()
		id := f.nextID
		f.nextID++
		p := Product{ID: id, Code: in.Code, Name: in.Name, Spec: in.Spec, Unit: in.Unit, Status: 1, Version: 1}
		f.products = append(f.products, p)
		f.mu.Unlock()

		if f.slowCreate > 0 {
			// 模拟"服务端已经写入，但响应回不来"。
			time.Sleep(f.slowCreate)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p)

	case r.URL.Path == "/api/products" && r.Method == http.MethodDelete:
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		f.mu.Lock()
		kept := f.products[:0]
		for _, p := range f.products {
			if p.ID != id {
				kept = append(kept, p)
			}
		}
		f.products = kept
		delete(f.boms, id)
		f.mu.Unlock()
		fmt.Fprint(w, `{"status":"ok"}`)

	case r.URL.Path == "/api/bom" && r.Method == http.MethodPost:
		if f.failBOM {
			writeFakeErr(w, http.StatusBadRequest, "零件不存在")
			return
		}
		var in BOMInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeFakeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		f.mu.Lock()
		item := BOMItem{ID: int64(len(f.boms[in.ProductID]) + 1), ProductID: in.ProductID, PartID: in.PartID,
			Quantity: in.Quantity, LossRate: in.LossRate, UseMode: in.UseMode}
		f.boms[in.ProductID] = append(f.boms[in.ProductID], item)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(item)

	case r.URL.Path == "/api/bom" && r.Method == http.MethodGet:
		pid, _ := strconv.ParseInt(r.URL.Query().Get("product_id"), 10, 64)
		f.mu.Lock()
		items := f.boms[pid]
		f.mu.Unlock()
		if len(items) == 0 {
			fmt.Fprint(w, "null")
			return
		}
		_ = json.NewEncoder(w).Encode(items)

	default:
		writeFakeErr(w, http.StatusNotFound, "not found")
	}
}

func writeFakeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func startFake(t *testing.T, f *fakeReef) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestProbeParsesCamelCase(t *testing.T) {
	c := startFake(t, newFakeReef())
	info, err := c.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.APIVersion != 1 || info.SchemaVersion != 13 || info.DatabaseID != "db-test" || info.Storage != "sqlite" {
		t.Fatalf("字段没解出来，检查 json tag 大小写: %+v", info)
	}
}

func TestListProductsToleratesNull(t *testing.T) {
	c := startFake(t, newFakeReef())
	products, err := c.ListProducts(context.Background(), "t0")
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if products == nil {
		t.Fatal("零行必须是空切片，不能是 nil——否则前端 .map 会白屏")
	}
	if len(products) != 0 {
		t.Fatalf("期望 0 条，得到 %d", len(products))
	}
}

func TestCreateProductSendsOnlyWhitelistedFields(t *testing.T) {
	var got map[string]any
	inner := newFakeReef()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/products" && r.Method == http.MethodPost {
			var probe map[string]any
			body := json.NewDecoder(r.Body)
			if err := body.Decode(&probe); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got = probe
			fmt.Fprint(w, `{"id":1,"code":"OPP-1","name":"x","unit":"台","status":1,"version":1}`)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreateProduct(context.Background(), "t0", CreateProductInput{
		Code: "OPP-1", Name: "x", Spec: "来源", Unit: "台", Status: 1,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	allowed := map[string]bool{"code": true, "name": true, "spec": true, "unit": true, "status": true, "id": true, "version": true}
	for k := range got {
		if !allowed[k] {
			t.Fatalf("请求体里出现了 RFERP 不接受的字段 %q —— DisallowUnknownFields 会直接 400", k)
		}
	}
	if len(got) == 0 {
		t.Fatal("没有捕获到请求体")
	}
}

func TestAdoptHappyPath(t *testing.T) {
	f := newFakeReef()
	c := startFake(t, f)

	res, err := c.Adopt(context.Background(), "t0", AdoptInput{
		OpportunityID: 7,
		Name:          "便携咖啡机",
		Lines: []BOMLine{
			{PartID: 3, Qty: 2, LossRate: 3},
			{PartID: 0, Qty: 1}, // 临时零件，应计入 skipped
			{PartID: 5, Qty: 1},
		},
	})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if res.Status != StatusOK || res.BOMCreated != 2 || res.BOMSkipped != 1 {
		t.Fatalf("结果不对: %+v", res)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.products) != 1 {
		t.Fatalf("期望 1 个产品，得到 %d", len(f.products))
	}
	if f.products[0].Code != "OPP-7" {
		t.Fatalf("默认编码应为 OPP-7，得到 %q", f.products[0].Code)
	}
	if f.products[0].Spec == nil || *f.products[0].Spec != Marker(7) {
		t.Fatalf("spec 必须留溯源标记，得到 %v", f.products[0].Spec)
	}
}

func TestAdoptRollsBackWhenBOMFails(t *testing.T) {
	f := newFakeReef()
	f.failBOM = true
	c := startFake(t, f)

	res, err := c.Adopt(context.Background(), "t0", AdoptInput{
		OpportunityID: 9,
		Name:          "折叠露营灯",
		Lines:         []BOMLine{{PartID: 3, Qty: 1}},
	})
	if err == nil {
		t.Fatal("建 BOM 失败时必须返回错误")
	}
	if res.Status != StatusRolledBack {
		t.Fatalf("期望 rolled_back，得到 %+v", res)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.products) != 0 {
		t.Fatalf("回滚后不应留下产品，实际 %d 个", len(f.products))
	}
}

func TestAdoptReconcilesAfterTimeout(t *testing.T) {
	f := newFakeReef()
	f.slowCreate = 120 * time.Millisecond
	srv := httptest.NewServer(f)
	defer srv.Close()

	c := NewWithHTTPClient(srv.URL, &http.Client{Timeout: 40 * time.Millisecond})
	res, err := c.Adopt(context.Background(), "t0", AdoptInput{
		OpportunityID: 11,
		Name:          "便携咖啡机",
		Lines:         []BOMLine{{PartID: 3, Qty: 1}},
	})
	if err != nil {
		t.Fatalf("超时后应通过回查继续，实际: %v", err)
	}
	if res.Status != StatusOK {
		t.Fatalf("期望 ok，得到 %+v", res)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.products) != 1 {
		t.Fatalf("超时后重试必须复用已创建的产品，不能建第二条；实际 %d 条", len(f.products))
	}
}

func TestAdoptRefusesCodeTakenByOthers(t *testing.T) {
	f := newFakeReef()
	spec := "别的产品"
	f.products = append(f.products, Product{ID: 99, Code: "OPP-7", Name: "占位", Spec: &spec})
	c := startFake(t, f)

	res, err := c.Adopt(context.Background(), "t0", AdoptInput{OpportunityID: 7, Name: "便携咖啡机"})
	if !errors.Is(err, ErrCodeTaken) {
		t.Fatalf("同编码但不是本候选的产品，应返回 ErrCodeTaken，实际: %v (%+v)", err, res)
	}
}

func TestAdoptIsIdempotent(t *testing.T) {
	f := newFakeReef()
	c := startFake(t, f)
	in := AdoptInput{OpportunityID: 7, Name: "便携咖啡机"}

	if _, err := c.Adopt(context.Background(), "t0", in); err != nil {
		t.Fatalf("第一次采纳: %v", err)
	}
	res, err := c.Adopt(context.Background(), "t0", in)
	var exists *ErrProductExists
	if !errors.As(err, &exists) {
		t.Fatalf("第二次应返回 ErrProductExists，实际: %v", err)
	}
	if res.Status != StatusExists {
		t.Fatalf("期望 exists，得到 %+v", res)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.products) != 1 {
		t.Fatalf("重复采纳不能产生第二个产品，实际 %d 个", len(f.products))
	}
}

func TestDeviceHeaderIsSentOnEveryRequest(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-RFERP-Device-ID"))
		switch r.URL.Path {
		case "/api/login":
			fmt.Fprint(w, `{"token":"t0","user":{"id":1,"username":"admin","role":"admin"}}`)
		case "/api/me":
			fmt.Fprint(w, `{"id":1,"username":"admin","role":"admin"}`)
		default:
			fmt.Fprint(w, `null`)
		}
	}))
	defer srv.Close()

	c := New(srv.URL).WithDevice("dev-abc")
	ctx := context.Background()
	if _, err := c.Login(ctx, "admin", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := c.Me(ctx, "t0"); err != nil {
		t.Fatalf("me: %v", err)
	}
	for i, v := range got {
		if v != "dev-abc" {
			t.Fatalf("第 %d 个请求没带设备标识（RFERP 2.1.1 会返回 401）: %q", i+1, v)
		}
	}
}

func TestAdoptRollbackReclaimsParts(t *testing.T) {
	f := newFakeReef()
	f.failBOM = true
	c := startFake(t, f)

	res, err := c.Adopt(context.Background(), "t0", AdoptInput{
		OpportunityID: 11, Name: "回收测试",
		Parts: []PartSpec{{Name: "临时零件", Code: "P-1", Unit: "个"}},
		Lines: []BOMLine{{PartRef: "临时零件", Qty: 1}},
	})
	if err == nil || res.Status != StatusRolledBack {
		t.Fatalf("应回滚：%+v err=%v", res, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.products) != 0 {
		t.Fatalf("产品应被删：%d", len(f.products))
	}
	if len(f.parts) != 0 {
		t.Fatalf("本次新建零件应被回收：%d", len(f.parts))
	}
}

func TestAdoptRollbackKeepsReferencedPart(t *testing.T) {
	f := newFakeReef()
	f.failBOM = true
	f.partKept = true
	c := startFake(t, f)

	res, err := c.Adopt(context.Background(), "t0", AdoptInput{
		OpportunityID: 12, Name: "保留测试",
		Parts: []PartSpec{{Name: "被引用零件", Code: "P-2", Unit: "个"}},
		Lines: []BOMLine{{PartRef: "被引用零件", Qty: 1}},
	})
	if err == nil || res.Status != StatusRolledBack {
		t.Fatalf("应回滚：%+v err=%v", res, err)
	}
	if !strings.Contains(res.Message, "零件已保留") {
		t.Fatalf("应说明零件已保留：%s", res.Message)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.parts) != 1 {
		t.Fatalf("被引用的零件应保留：%d", len(f.parts))
	}
}
