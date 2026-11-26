package reef

import "time"

// ServerInfo 是 GET /api/v1/serverinfo 的响应。
//
// 注意字段是 camelCase，且 APIVersion 是整数——RFERP 的这个端点与其余业务端点
// 使用了两套命名风格，照着 snake_case 写会全部解出零值。
type ServerInfo struct {
	AppVersion    string `json:"appVersion"`
	APIVersion    int    `json:"apiVersion"`
	SchemaVersion int    `json:"schemaVersion"`
	DatabaseID    string `json:"databaseId"`
	Storage       string `json:"storage"`
}

// SupportedAPIVersion 是 radar 验证过的接口版本。
const SupportedAPIVersion = 1

type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role"`
	Status      int    `json:"status"`
}

const RoleAdmin = "admin"

type Product struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Spec      *string   `json:"spec,omitempty"`
	Unit      string    `json:"unit"`
	Status    int       `json:"status"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Operator  *string   `json:"operator,omitempty"`
}

type Part struct {
	ID       int64   `json:"id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Spec     *string `json:"spec,omitempty"`
	Unit     string  `json:"unit"`
	PartType *string `json:"part_type,omitempty"`
	StockQty float64 `json:"stock_qty"`
	WarnQty  float64 `json:"warn_qty"`
	Status   int     `json:"status"`
	Supplier *string `json:"supplier,omitempty"`
}

type BOMItem struct {
	ID          int64   `json:"id"`
	ProductID   int64   `json:"product_id"`
	PartID      int64   `json:"part_id"`
	Quantity    float64 `json:"quantity"`
	LossRate    float64 `json:"loss_rate"`
	Remark      *string `json:"remark,omitempty"`
	Replaceable int     `json:"replaceable"`
	UseMode     int     `json:"use_mode"`
	PartCode    *string `json:"part_code,omitempty"`
	PartName    *string `json:"part_name,omitempty"`
}

// LoginResult 是 POST /api/login 的响应。
type LoginResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// CreateProductInput 刻意只保留 RFERP 允许的字段。
//
// RFERP 的 decodeProductWrite 用了 DisallowUnknownFields()：多一个字段就 400。
// 因此这里**不要**为了"以后可能有用"加 Source / Operator / CreatedAt——
// 加了就会让采纳流程在第一步失败，而且报错信息很难联想到是本地的结构体多了一个字段。
type CreateProductInput struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Spec   string `json:"spec,omitempty"`
	Unit   string `json:"unit"`
	Status int    `json:"status"`
}

type BOMInput struct {
	ProductID   int64   `json:"product_id"`
	PartID      int64   `json:"part_id"`
	Quantity    float64 `json:"quantity"`
	LossRate    float64 `json:"loss_rate"`
	UseMode     int     `json:"use_mode"`
	Replaceable int     `json:"replaceable"`
}
