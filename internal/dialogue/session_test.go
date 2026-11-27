package dialogue

import "testing"

func TestSessionOneShot(t *testing.T) {
	var s Session
	r := s.Turn("我想看便携咖啡机，卖到北美，大概 $50，长期代工")
	if r.Kind != KindReady {
		t.Fatalf("一句话给全信息应直接 ready，得到 %+v", r)
	}
	if r.Plan == nil || r.Plan.Keyword != "便携咖啡机" || r.Plan.Market != "na" {
		t.Fatalf("槽位抽取错误: %+v", r.Plan)
	}
	if r.Plan.TargetUSD == nil || *r.Plan.TargetUSD != 55 {
		t.Fatalf("$50 应落到 30–80 带（中值 55），得到 %v", r.Plan.TargetUSD)
	}
}

func TestSessionExtractsExplicitUnknownCategory(t *testing.T) {
	var s Session
	reply := s.Turn("品类：手摇磨豆机，卖到欧洲，售价 $50，长期代工")
	if reply.Kind != KindReady || reply.Plan.Keyword != "手摇磨豆机" {
		t.Fatalf("explicit category was not used: %+v", reply)
	}
}

func TestSessionMultiTurn(t *testing.T) {
	var s Session
	steps := []struct {
		in   string
		want string
	}{
		{"想看看露营这块", KindQuestion},
		{"卖到欧洲", KindQuestion},
		{"$120 左右", KindQuestion},
		{"还没有固定供应商", KindReady},
	}
	for i, st := range steps {
		r := s.Turn(st.in)
		if r.Kind != st.want {
			t.Fatalf("第 %d 轮 %q：期望 %s，得到 %+v", i+1, st.in, st.want, r)
		}
	}
}

func TestSessionRefusesOffTopic(t *testing.T) {
	var s Session
	for _, in := range []string{"今天天气怎么样", "帮我写一首诗", "你是谁"} {
		r := s.Turn(in)
		if r.Kind != KindRefused {
			t.Fatalf("%q 应被拒绝回答，得到 %+v", in, r)
		}
	}
}

func TestSessionKeepsTopicOnConcernOnly(t *testing.T) {
	var s Session
	r := s.Turn("认证麻烦吗")
	if r.Kind != KindQuestion {
		t.Fatalf("只提顾虑时应继续问缺的槽位，而不是拒答: %+v", r)
	}
	if len(r.Missing) == 0 || r.Missing[0] != "direction" {
		t.Fatalf("应先问品类，得到 %v", r.Missing)
	}
}

func TestDetectPrice(t *testing.T) {
	cases := map[string]string{
		"$20":    "usd_lt30",
		"50 美元":  "usd_30_80",
		"大概 100": "usd_80_150",
		"200 美金": "usd_gt150",
		"30-80":  "usd_30_80",
		"没有提到价格": "",
	}
	for in, want := range cases {
		if got := detectPrice(in); got != want {
			t.Errorf("%q: 期望 %q，得到 %q", in, want, got)
		}
	}
}
