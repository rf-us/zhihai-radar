package reef

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// 采纳结果状态。与前端展示一一对应。
const (
	StatusOK         = "ok"          // 产品与 BOM 都写入成功
	StatusExists     = "exists"      // 之前已经采纳过，本次未重复写入
	StatusRolledBack = "rolled_back" // 中途失败，已删除产品回滚
	StatusPartial    = "partial"     // 失败且回滚失败，需要人工处理
	StatusFailed     = "failed"      // 未产生任何写入
)

type BOMLine struct {
	PartID   int64
	Qty      float64
	LossRate float64
	UseMode  int
	// PartRef 指向 Parts 里的零件名；PartID 为 0 时用它匹配新建的零件。
	PartRef string
}

// PartSpec 是采纳时要新建的零件。Supplier 来自选定的供应链报价。
type PartSpec struct {
	Name     string
	Code     string
	Unit     string
	Supplier string
}

type AdoptInput struct {
	OpportunityID int64
	Code          string
	Name          string
	Unit          string
	Lines         []BOMLine
	// Parts 非空时，先建零件再建 BOM；PartRef 与 Parts[i].Name 对应。
	Parts []PartSpec
}

type AdoptResult struct {
	Status       string `json:"status"`
	ProductID    int64  `json:"product_id,omitempty"`
	BOMCreated   int    `json:"bom_created"`
	BOMSkipped   int    `json:"bom_skipped"`
	PartsCreated int    `json:"parts_created,omitempty"`
	Message      string `json:"message,omitempty"`
}

// Marker 是写进产品 spec 的溯源标记。
//
// 这一行让 RFERP 的产品页自己就能说明来源，不依赖 radar 是否在线。
func Marker(opportunityID int64) string {
	return fmt.Sprintf("来源：知乎选品雷达 #%d", opportunityID)
}

// Adopt 把一个候选采纳为 RFERP 的产品与 BOM。
//
// 跨服务没有事务，所以这里是一个 Saga：
//
//	回查 → 建产品 → 逐条建 BOM → 任一失败则删除产品
//
// 两个必须处理的细节：
//  1. 建产品超时属于"结果未知"，不能直接重试，要先按 code + spec 回查；
//  2. 建 BOM 失败时删除产品是安全的——BOM 随外键级联删除，且此时还没有批次引用它。
func (c *Client) Adopt(ctx context.Context, token string, in AdoptInput) (AdoptResult, error) {
	in = in.normalized()
	res := AdoptResult{Status: StatusFailed}

	if in.Name == "" {
		return res, errors.New("reef: 产品名不能为空")
	}

	existing, err := c.findOwnedProduct(ctx, token, in.Code, in.OpportunityID)
	if err != nil {
		res.Message = err.Error()
		if errors.Is(err, ErrCodeTaken) {
			res.Message = "编码 " + in.Code + " 已被 RFERP 中的其他产品占用，请换一个"
		}
		return res, err
	}
	if existing != nil {
		res.Status = StatusExists
		res.ProductID = existing.ID
		res.Message = "该候选之前已采纳，未重复创建"
		return res, &ErrProductExists{ProductID: existing.ID, Code: in.Code}
	}

	product, err := c.CreateProduct(ctx, token, CreateProductInput{
		Code:   in.Code,
		Name:   in.Name,
		Spec:   Marker(in.OpportunityID),
		Unit:   in.Unit,
		Status: 1,
	})
	if err != nil {
		if IsUnknownOutcome(err) {
			// 可能已经建成。回查一次，找到就继续，找不到才算失败。
			found, findErr := c.findOwnedProduct(ctx, token, in.Code, in.OpportunityID)
			if findErr == nil && found != nil {
				product = *found
			} else {
				res.Message = "创建产品结果未知，回查未找到：" + err.Error()
				return res, err
			}
		} else {
			res.Message = err.Error()
			return res, err
		}
	}

	// 建零件（可选）：选定供应商的报价带过来，写进 RFERP 的 parts.supplier。
	// 建零件失败同样删产品回滚——此时产品还没有 BOM，也没有批次引用。
	partIDs := make(map[string]int64, len(in.Parts))
	for i, spec := range in.Parts {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		code := strings.TrimSpace(spec.Code)
		if code == "" {
			code = fmt.Sprintf("P-%d-%d", in.OpportunityID, i+1)
		}
		unit := strings.TrimSpace(spec.Unit)
		if unit == "" {
			unit = "个"
		}
		var supplier *string
		if s := strings.TrimSpace(spec.Supplier); s != "" {
			supplier = &s
		}
		part, err := c.CreatePart(ctx, token, CreatePartInput{
			Code: code, Name: name, Unit: unit, Supplier: supplier,
		})
		if err != nil {
			delErr := c.DeleteProduct(ctx, token, product.ID)
			if delErr != nil {
				res.Status = StatusPartial
				res.ProductID = product.ID
				res.Message = fmt.Sprintf("建零件失败（%v），回滚也失败（%v）：产品 %d 需手工删除", err, delErr, product.ID)
				return res, err
			}
			res.Status = StatusRolledBack
			res.Message = fmt.Sprintf("建零件失败（%v），已删除刚创建的产品 %d", err, product.ID)
			return res, err
		}
		partIDs[name] = part.ID
		res.PartsCreated++
	}

	lines := make([]BOMLine, len(in.Lines))
	copy(lines, in.Lines)
	for i := range lines {
		if lines[i].PartID <= 0 && lines[i].PartRef != "" {
			lines[i].PartID = partIDs[lines[i].PartRef]
		}
	}

	for _, line := range lines {
		if line.PartID <= 0 {
			// 手填的临时零件只参与毛利测算，不进 BOM。
			res.BOMSkipped++
			continue
		}
		_, err := c.AddBOMItem(ctx, token, BOMInput{
			ProductID:   product.ID,
			PartID:      line.PartID,
			Quantity:    line.Qty,
			LossRate:    line.LossRate,
			UseMode:     line.UseMode,
			Replaceable: 0,
		})
		if err != nil {
			delErr := c.DeleteProduct(ctx, token, product.ID)
			if delErr != nil {
				res.Status = StatusPartial
				res.ProductID = product.ID
				res.Message = fmt.Sprintf(
					"建 BOM 失败（%v），回滚也失败（%v）：产品 %d 已留在 RFERP，需手工删除",
					err, delErr, product.ID)
				return res, err
			}
			res.Status = StatusRolledBack
			res.Message = fmt.Sprintf("建 BOM 失败（%v），已删除刚创建的产品 %d", err, product.ID)
			return res, err
		}
		res.BOMCreated++
	}

	res.Status = StatusOK
	res.ProductID = product.ID
	res.Message = fmt.Sprintf("已创建产品 %s（#%d）与 %d 条 BOM", in.Code, product.ID, res.BOMCreated)
	return res, nil
}

// AdoptPreview 是采纳预检：只读，不产生任何写入。
//
// 界面在用户点"采纳"之前调用它，把"将要在哪台 RFERP 上创建什么"说清楚——
// 这比事后报错有用得多，尤其是编码冲突和权限不足这两种。
type AdoptPreview struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Unit          string `json:"unit"`
	Marker        string `json:"marker"`
	ProductExists bool   `json:"product_exists"`
	ProductID     int64  `json:"product_id,omitempty"`
	CodeTaken     bool   `json:"code_taken"`
	BOMCount      int    `json:"bom_count"`
	BOMSkipped    int    `json:"bom_skipped"`
	PartsToCreate int    `json:"parts_to_create"`
}

func (c *Client) AdoptPreview(ctx context.Context, token string, in AdoptInput) (AdoptPreview, error) {
	in = in.normalized()
	out := AdoptPreview{
		Code:          in.Code,
		Name:          in.Name,
		Unit:          in.Unit,
		Marker:        Marker(in.OpportunityID),
		PartsToCreate: len(in.Parts),
	}
	for _, line := range in.Lines {
		if line.PartID <= 0 {
			out.BOMSkipped++
			continue
		}
		out.BOMCount++
	}

	existing, err := c.findOwnedProduct(ctx, token, in.Code, in.OpportunityID)
	if err != nil {
		if errors.Is(err, ErrCodeTaken) {
			out.CodeTaken = true
			return out, nil
		}
		return out, err
	}
	if existing != nil {
		out.ProductExists = true
		out.ProductID = existing.ID
	}
	return out, nil
}

func (c *Client) findOwnedProduct(ctx context.Context, token, code string, opportunityID int64) (*Product, error) {
	products, err := c.ListProducts(ctx, token)
	if err != nil {
		return nil, err
	}
	return matchOwned(products, code, opportunityID)
}

// matchOwned 在"编码允许重复"的前提下判断某个产品是不是本候选之前采纳出来的。
//
// RFERP 的 products.code 是系列号，允许重复，所以不能"看到同编码就认为已采纳"，
// 必须再看 spec 里的溯源标记。
func matchOwned(products []Product, code string, opportunityID int64) (*Product, error) {
	codeSeen := false
	for i := range products {
		if products[i].Code != code {
			continue
		}
		codeSeen = true
		if ownedBy(&products[i], opportunityID) {
			return &products[i], nil
		}
	}
	if codeSeen {
		return nil, ErrCodeTaken
	}
	return nil, nil
}

func ownedBy(p *Product, opportunityID int64) bool {
	if p.Spec == nil {
		return false
	}
	return strings.Contains(*p.Spec, fmt.Sprintf("#%d", opportunityID))
}

func (in AdoptInput) normalized() AdoptInput {
	in.Code = strings.TrimSpace(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.Unit = strings.TrimSpace(in.Unit)
	if in.Code == "" {
		in.Code = fmt.Sprintf("OPP-%d", in.OpportunityID)
	}
	if in.Unit == "" {
		in.Unit = "台"
	}
	return in
}
