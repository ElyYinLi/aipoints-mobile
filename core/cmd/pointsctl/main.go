// pointsctl 是引擎的命令行外壳，用于在没有手机的情况下验证接口链路。
//
// 用法示例：
//
//	pointsctl providers
//	pointsctl query   -provider workbuddy -token $WB_TOKEN -uid u123
//	pointsctl query   -provider lingxi   -cookie "$KDOCS_COOKIE"
//	pointsctl checkin -provider dumate   -cookie "$BAIDU_COOKIE"
//	pointsctl checkin -provider aipy     -cookie "$TRUSTOKEN_COOKIE"
//
// 所有输出默认是人类可读；加 -json 输出机器可读 JSON（便于管道与断言）。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/mobile"
	"github.com/ElyYinLi/aipoints-mobile/core/panel"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

func main() {
	var (
		action     = flag.String("action", "query", "query | checkin | providers")
		providerID = flag.String("provider", "", "平台 id: workbuddy | lingxi | dumate | trae | aipy")
		token      = flag.String("token", "", "access token（token 型平台）")
		cookie     = flag.String("cookie", "", "cookie 串（cookie 型平台）")
		uid        = flag.String("uid", "", "X-User-Id（WorkBuddy）")
		entID      = flag.String("enterprise", "", "X-Enterprise-Id（WorkBuddy）")
		domain     = flag.String("domain", "", "覆盖默认域名")
		asJSON     = flag.Bool("json", false, "输出 JSON")
		timeout    = flag.Duration("timeout", 25*time.Second, "单次请求超时")
		dataDir    = flag.String("dir", "./data", "数据目录（serve/add 用）")
		addr       = flag.String("addr", "127.0.0.1:7879", "面板监听地址（serve 用）")
	)
	flag.Parse()

	reg := provider.NewRegistry(httpx.New())

	if *action == "providers" {
		for _, m := range reg.All() {
			fmt.Printf("%-10s %-12s auth=%-7s mobile=%-11s %s\n", m.ID, m.Name, m.Auth, m.Mobile, m.Note)
		}
		return
	}

	// serve：本机起面板，浏览器/手机 WebView 直接访问（Linux 上可先跑通 UI）
	if *action == "serve" {
		st, err := store.NewFileStore(*dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "初始化存储失败:", err)
			os.Exit(1)
		}
		// Version 留空会让设置页显示「版本 —」，调试时看不出跑的是哪一版
		srv := panel.New(panel.Options{Addr: *addr, Store: st, Reg: reg, DataDir: *dataDir, Version: mobile.Version})
		if err := srv.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("面板已启动: http://%s\n数据目录: %s\n", *addr, *dataDir)
		select {} // 阻塞直到被杀
	}

	// add：把凭据写进本地账号库（桌面/调试用；移动端请用 Keychain）
	if *action == "add" {
		st, err := store.NewFileStore(*dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "初始化存储失败:", err)
			os.Exit(1)
		}
		cred := provider.Credential{
			AccountID:    flag.Arg(0),
			Label:        flag.Arg(1),
			AccessToken:  *token,
			Cookie:       *cookie,
			UID:          *uid,
			EnterpriseID: *entID,
			Domain:       *domain,
			Extra:        map[string]string{"source": "pointsctl"},
		}
		if cred.AccountID == "" {
			fmt.Fprintln(os.Stderr, "用法: pointsctl add -provider X [-token T|-cookie C] <account_id> [label]")
			os.Exit(2)
		}
		secret, _ := json.Marshal(cred)
		if err := st.Put(store.Account{
			Provider: *providerID, AccountID: cred.AccountID, Label: cred.Label,
			SecretJSON: secret, Enabled: true,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "写入失败:", err)
			os.Exit(1)
		}
		fmt.Printf("已写入 %s/%s\n", *providerID, cred.AccountID)
		return
	}

	if *providerID == "" && *action != "serve" && *action != "add" {
		fmt.Fprintln(os.Stderr, "缺少 -provider")
		os.Exit(2)
	}
	p, ok := reg.Get(*providerID)
	if !ok && *providerID != "" {
		fmt.Fprintf(os.Stderr, "未知平台: %s\n", *providerID)
		os.Exit(2)
	}

	cred := provider.Credential{
		AccountID:    "cli",
		AccessToken:  *token,
		Cookie:       *cookie,
		UID:          *uid,
		EnterpriseID: *entID,
		Domain:       *domain,
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch *action {
	case "query":
		snap, err := p.Query(ctx, cred)
		if err != nil {
			fmt.Fprintln(os.Stderr, "查询失败:", err)
			os.Exit(1)
		}
		if *asJSON {
			printJSON(snap)
			return
		}
		fmt.Printf("平台: %s\n余额: %s\n今日已签: %v\n连续/进度: %d\n说明: %s\n",
			snap.Provider, snap.BalanceText, snap.CheckedIn, snap.Streak, firstNonEmpty(snap.Detail, snap.Error))
	case "checkin":
		res, err := p.Checkin(ctx, cred)
		if err != nil {
			fmt.Fprintln(os.Stderr, "签到失败:", err)
			os.Exit(1)
		}
		if *asJSON {
			printJSON(res)
			return
		}
		fmt.Printf("平台: %s\n成功: %v\n是否已签: %v\n获得: %v\n信息: %s\n",
			res.Provider, res.OK, res.Already, res.Gain, firstNonEmpty(res.Message, res.Error))
	default:
		fmt.Fprintln(os.Stderr, "未知 action:", *action)
		os.Exit(2)
	}
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
