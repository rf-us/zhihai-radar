//go:build live

// 真 RFERP 的契约测试。默认不跑（没有真实例时应该跳过而不是失败）。
//
// 运行方式：
//
//	REEF_BASE=http://127.0.0.1:8080 REEF_USER=admin REEF_PASS=*** \
//	  go test ./internal/reef -tags=live -v
//
// 它会真的建一个产品、加一条 BOM、再删掉。**不要指向生产库**：
// 编码用时间戳生成，删除在 cleanup 里保证执行，但中途断网仍可能留下一条。
package reef

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestLiveContract(t *testing.T) {
	base := os.Getenv("REEF_BASE")
	user := os.Getenv("REEF_USER")
	pass := os.Getenv("REEF_PASS")
	if base == "" || user == "" || pass == "" {
		t.Skip("未设置 REEF_BASE / REEF_USER / REEF_PASS，跳过真实例契约测试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c := New(base)
	if id := os.Getenv("REEF_DEVICE_ID"); id != "" {
		c = c.WithDevice(id)
	}

	info, err := c.Probe(ctx)
	if err != nil {
		t.Fatalf("serverinfo: %v", err)
	}
	t.Logf("RFERP app=%s api=%d schema=%d storage=%s db=%s",
		info.AppVersion, info.APIVersion, info.SchemaVersion, info.Storage, info.DatabaseID)
	if info.APIVersion != SupportedAPIVersion {
		t.Fatalf("接口版本 %d 与 radar 验证过的 %d 不一致", info.APIVersion, SupportedAPIVersion)
	}

	sess, err := c.Login(ctx, user, pass)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	me, err := c.Me(ctx, sess.Token)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.Role != RoleAdmin {
		t.Skipf("当前账号角色是 %s，建产品会被 403 拒绝，跳过写操作", me.Role)
	}

	parts, err := c.ListParts(ctx, sess.Token)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	t.Logf("零件 %d 个", len(parts))

	code := fmt.Sprintf("OPP-LIVE-%d", time.Now().Unix())
	marker := Marker(999999)
	product, err := c.CreateProduct(ctx, sess.Token, CreateProductInput{
		Code: code, Name: "契约测试产品", Spec: marker, Unit: "台", Status: 1,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.DeleteProduct(cleanupCtx, sess.Token, product.ID); err != nil {
			t.Logf("清理失败，请手工删除产品 %d：%v", product.ID, err)
		}
	})
	if product.ID == 0 {
		t.Fatal("创建产品未返回 id")
	}
	if product.Spec == nil || *product.Spec != marker {
		t.Fatalf("spec 溯源标记未保留: %v", product.Spec)
	}

	// 列表里要能按 code 找到它——采纳前的去重就靠这个。
	products, err := c.ListProducts(ctx, sess.Token)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	found := false
	for _, p := range products {
		if p.Code == code {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("新建的产品没有出现在列表里（code=%s）", code)
	}

	if len(parts) == 0 {
		t.Log("没有零件，跳过 BOM 写入")
		return
	}
	item, err := c.AddBOMItem(ctx, sess.Token, BOMInput{
		ProductID: product.ID, PartID: parts[0].ID, Quantity: 1, LossRate: 0, UseMode: 0,
	})
	if err != nil {
		t.Fatalf("add bom: %v", err)
	}
	bom, err := c.GetBOM(ctx, sess.Token, product.ID)
	if err != nil {
		t.Fatalf("get bom: %v", err)
	}
	if len(bom) != 1 || bom[0].ID != item.ID {
		t.Fatalf("BOM 读回不一致: %+v", bom)
	}
}

// TestLiveAdoptWithParts 真实例验证扩展后的采纳：建零件（带供应商）→ 建产品 →
// 建 BOM → 读回 → 删产品 → 确认可用。设备指纹从环境变量读，不打印到日志。
//
// 与 TestLiveContract 共用 REEF_BASE/USER/PASS/DEVICE_ID；不设置则跳过。
func TestLiveAdoptWithParts(t *testing.T) {
	base := os.Getenv("REEF_BASE")
	user := os.Getenv("REEF_USER")
	pass := os.Getenv("REEF_PASS")
	device := os.Getenv("REEF_DEVICE_ID")
	if base == "" || user == "" || pass == "" || device == "" {
		t.Skip("未设置 REEF_BASE/USER/PASS/DEVICE_ID，跳过真实例采纳测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := New(base).WithDevice(device)

	sess, err := c.Login(ctx, user, pass)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	me, err := c.Me(ctx, sess.Token)
	if err != nil || me.Role != RoleAdmin {
		t.Skipf("需要管理员账号，当前角色=%v err=%v", me.Role, err)
	}

	oid := int64(999998)
	code := fmt.Sprintf("OPP-LIVE-P-%d", time.Now().Unix())
	supplier := "契约测试供应商"
	partCode := fmt.Sprintf("PL-LIVE-%d", time.Now().Unix())

	part, err := c.CreatePart(ctx, sess.Token, CreatePartInput{
		Code: partCode, Name: "契约测试零件", Unit: "个", Supplier: &supplier,
	})
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	t.Cleanup(func() {
		// 零件删除端点不是 radar 的依赖面；这里只清理产品，零件留在一次性实例里。
		_ = part
	})

	res, err := c.Adopt(ctx, sess.Token, AdoptInput{
		OpportunityID: oid, Code: code, Name: "契约测试采纳产品", Unit: "台",
		Parts: []PartSpec{{Name: "契约测试零件", Code: partCode, Unit: "个", Supplier: supplier}},
		Lines: []BOMLine{{PartID: part.ID, Qty: 1, LossRate: 0, UseMode: 0}},
	})
	if err != nil {
		t.Fatalf("adopt: %v status=%s msg=%s", err, res.Status, res.Message)
	}
	if res.Status != StatusOK || res.ProductID == 0 || res.PartsCreated != 1 || res.BOMCreated != 1 {
		t.Fatalf("采纳结果不符：%+v", res)
	}
	t.Cleanup(func() {
		cc, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.DeleteProduct(cc, sess.Token, res.ProductID); err != nil {
			t.Logf("清理产品失败，请手工删除 %d：%v", res.ProductID, err)
		}
	})

	// 第二次采纳同一机会应幂等，不产生新数据。
	second, err := c.Adopt(ctx, sess.Token, AdoptInput{
		OpportunityID: oid, Code: code, Name: "契约测试采纳产品", Unit: "台",
	})
	if err == nil || second.Status != StatusExists {
		t.Fatalf("第二次采纳应返回 exists：%+v err=%v", second, err)
	}
}
