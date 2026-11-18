package panel

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/provider"
)

// handleLotteryDrawAll 一键抽奖：对所有启用且支持抽奖的账号，把剩余次数全部抽掉。
//
// POST /api/lottery/draw_all
//
// 百度搭子每次签到送一次抽奖机会，签完顺便抽掉才是「一次点击全套做完」。
// 每个账号按 DrawStatus 拿到剩余次数后逐次抽，单次失败不影响其余；
// 抽奖是写请求，中间留间隔（同设备连续写最容易触发风控）。
func (s *Server) handleLotteryDrawAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	type drawRow struct {
		Provider  string   `json:"provider"`
		AccountID string   `json:"account_id"`
		Drawn     int      `json:"drawn"`
		Prizes    []string `json:"prizes,omitempty"`
		Gain      float64  `json:"gain"`
		Unit      string   `json:"unit,omitempty"`
		Error     string   `json:"error,omitempty"`
	}
	out := make([]drawRow, 0, len(list))
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			continue
		}
		lot, ok := p.(provider.LotteryProvider)
		if !ok {
			// 该平台没有抽奖体系，跳过
			continue
		}
		cred, err := decodeCred(a)
		if err != nil {
			out = append(out, drawRow{Provider: a.Provider, AccountID: a.AccountID, Error: "凭据损坏"})
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		st, serr := lot.DrawStatus(ctx, cred)
		cancel()
		if serr != nil {
			out = append(out, drawRow{Provider: a.Provider, AccountID: a.AccountID, Error: serr.Error()})
			continue
		}
		row := drawRow{Provider: a.Provider, AccountID: a.AccountID}
		if st.Remaining <= 0 {
			// 没有次数就静默跳过（不算错误，用户不需要看到）
			continue
		}
		// 上限保护：上游异常返回超大次数时不要死循环打爆接口
		times := st.Remaining
		if times > 10 {
			times = 10
		}
		var errs []string
		for i := 0; i < times; i++ {
			dctx, dcancel := context.WithTimeout(r.Context(), 25*time.Second)
			res, derr := lot.Draw(dctx, cred)
			dcancel()
			if derr != nil {
				res.Error = derr.Error()
			}
			if res.OK {
				row.Drawn++
				row.Gain += res.Gain
				if row.Unit == "" && res.Unit != "" {
					row.Unit = res.Unit
				}
				if res.Prize != "" {
					row.Prizes = append(row.Prizes, res.Prize)
				}
			} else {
				if res.Error != "" {
					errs = append(errs, res.Error)
				}
				break // 抽不动了（次数用完 / 风控），不用继续试
			}
			time.Sleep(700 * time.Millisecond)
		}
		if len(errs) > 0 && row.Drawn == 0 {
			row.Error = strings.Join(errs, "；")
		}
		if row.Drawn > 0 || row.Error != "" {
			out = append(out, row)
		}
	}
	writeJSON(w, map[string]any{"ok": true, "results": out})
}
