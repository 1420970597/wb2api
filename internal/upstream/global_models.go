// global 模型目录探测：目标目录固定为已验证的 19 个模型；上游探测只补充实时元数据。
//
// 探测两路并发：/v3/config（主路，IDE UA 完整能力版）+ 企业端点家族
// （/v2 → /console 补缺），并集 = v3 条目为主、企业端点补 v3 缺失的 id
// （如 gpt-5.3-codex 只在 /v2 下发）。倍率字段（credits）虽随目录下发，但
// 只透出展示，不注入 costTier、不参与选号。
//
// 上游目录偶发失败时仍返回这份已验证目录，避免客户端与管理面板的模型选择器
// 因一次目录请求失败而清空。实际请求仍由 global 账号路由，账号不可用时不会被选号。
package upstream

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// GlobalModelNames 国际版（global realm）的已验证目录。顺序就是面板展示顺序。
var GlobalModelNames = []string{
	"default-model",
	"fast-model",
	"balanced-model",
	"primary-model",
	"deep-model",
	"hy4-preview",
	"hy3",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.3-codex",
	"gemini-3.5-flash",
	"glm-5.3",
	"glm-5.2",
	"kimi-k3",
	"kimi-k2.6",
	"deepseek-v4.1-flash",
}

// globalModelCatalog 是目标目录的展示与能力兜底。数值来自已验证的 global
// 模型能力页；远端成功返回时由 overlayGlobalCatalog 以远端非空字段补充。
var globalModelCatalog = []ModelInfo{
	{ID: "default-model", Name: "Auto", ContextWindow: 176000, MaxTokens: 24000, Credits: "x0.79 credits"},
	{ID: "fast-model", Name: "Fast", ContextWindow: 200000, MaxTokens: 32000, Credits: "x0.34 credits", SupportsReasoning: true, Efforts: []string{"medium"}, DefaultEffort: "medium"},
	{ID: "balanced-model", Name: "Balanced", ContextWindow: 256000, MaxTokens: 32000, Credits: "x0.59 credits", SupportsReasoning: true, Efforts: []string{"medium"}, DefaultEffort: "medium"},
	{ID: "primary-model", Name: "Primary", ContextWindow: 272000, MaxTokens: 72000, Credits: "x3.31 credits", SupportsReasoning: true, Efforts: []string{"high"}, DefaultEffort: "high"},
	{ID: "deep-model", Name: "Deep", ContextWindow: 176000, MaxTokens: 24000, Credits: "x3.33 credits"},
	{ID: "hy4-preview", Name: "Hy4 preview", ContextWindow: 1000000, MaxTokens: 64000, Credits: "x0.00", SupportsReasoning: true, Efforts: []string{"high"}, DefaultEffort: "high"},
	{ID: "hy3", Name: "Hy3", ContextWindow: 192000, MaxTokens: 64000, Credits: "x0.00", SupportsReasoning: true, Efforts: []string{"low", "high"}, DefaultEffort: "high"},
	{ID: "gpt-5.6-sol", Name: "GPT-5.6-Sol", ContextWindow: 1000000, MaxTokens: 128000, Credits: "x3.47", SupportsReasoning: true, CanDisableThinking: true, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
	{ID: "gpt-5.6-terra", Name: "GPT-5.6-Terra", ContextWindow: 1000000, MaxTokens: 128000, Credits: "x1.39", SupportsReasoning: true, CanDisableThinking: true, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
	{ID: "gpt-5.6-luna", Name: "GPT-5.6-Luna", ContextWindow: 1000000, MaxTokens: 128000, Credits: "x0.14", SupportsReasoning: true, CanDisableThinking: true, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
	{ID: "gpt-5.5", Name: "GPT-5.5", ContextWindow: 1000000, MaxTokens: 128000, Credits: "x3.31", SupportsReasoning: true, Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high"},
	{ID: "gpt-5.4", Name: "GPT-5.4", ContextWindow: 272000, MaxTokens: 72000, Credits: "x1.65", SupportsReasoning: true, Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high"},
	{ID: "gpt-5.3-codex", Name: "GPT-5.3-Codex", ContextWindow: 272000, MaxTokens: 72000, Credits: "x1.25", SupportsReasoning: true, Efforts: []string{"medium"}, DefaultEffort: "medium"},
	{ID: "gemini-3.5-flash", Name: "Gemini-3.5-Flash", ContextWindow: 1000000, MaxTokens: 66000, Credits: "x0.99", SupportsReasoning: true, Efforts: []string{"medium"}, DefaultEffort: "medium"},
	{ID: "glm-5.3", Name: "GLM-5.3", ContextWindow: 1000000, MaxTokens: 48000, Credits: "x0.79", SupportsReasoning: true, CanDisableThinking: true, Efforts: []string{"low", "high", "max"}, DefaultEffort: "high"},
	{ID: "glm-5.2", Name: "GLM-5.2", ContextWindow: 1000000, MaxTokens: 48000, Credits: "x0.79", SupportsReasoning: true, CanDisableThinking: true, Efforts: []string{"high", "xhigh"}, DefaultEffort: "high"},
	{ID: "kimi-k3", Name: "Kimi-K3", ContextWindow: 1000000, MaxTokens: 32000, Credits: "x1.62", SupportsReasoning: true, Efforts: []string{"medium"}, DefaultEffort: "medium"},
	{ID: "kimi-k2.6", Name: "Kimi-K2.6", ContextWindow: 256000, MaxTokens: 32000, Credits: "x0.52", SupportsReasoning: true, Efforts: []string{"medium"}, DefaultEffort: "medium"},
	{ID: "deepseek-v4.1-flash", Name: "Deepseek-V4.1-Flash", ContextWindow: 1000000, MaxTokens: 128000, Credits: "x0.03 credits", SupportsReasoning: true, Efforts: []string{"high"}, DefaultEffort: "high"},
}

// GlobalModelInfos 返回目录的独立副本，避免调用方修改共享切片。
func GlobalModelInfos() []ModelInfo {
	out := make([]ModelInfo, len(globalModelCatalog))
	for i, mi := range globalModelCatalog {
		out[i] = cloneModelInfo(mi)
	}
	return out
}

func cloneModelInfo(mi ModelInfo) ModelInfo {
	mi.Efforts = append([]string(nil), mi.Efforts...)
	mi.Tags = append([]string(nil), mi.Tags...)
	return mi
}

// overlayGlobalCatalog 保持目标目录与顺序不变，仅用远端返回的非空字段刷新元数据。
func overlayGlobalCatalog(remote []ModelInfo) []ModelInfo {
	byID := make(map[string]ModelInfo, len(remote))
	for _, mi := range remote {
		byID[mi.ID] = mi
	}
	out := GlobalModelInfos()
	for i, base := range out {
		got, ok := byID[base.ID]
		if !ok {
			continue
		}
		if got.Name != "" {
			out[i].Name = got.Name
		}
		if got.ContextWindow > 0 {
			out[i].ContextWindow = got.ContextWindow
		}
		if got.MaxTokens > 0 {
			out[i].MaxTokens = got.MaxTokens
		}
		if len(got.Efforts) > 0 {
			out[i].Efforts = append([]string(nil), got.Efforts...)
		}
		if got.DefaultEffort != "" {
			out[i].DefaultEffort = got.DefaultEffort
		}
		if got.Description != "" {
			out[i].Description = got.Description
		}
		if got.Credits != "" {
			out[i].Credits = got.Credits
		}
		if len(got.Tags) > 0 {
			out[i].Tags = append([]string(nil), got.Tags...)
		}
		if got.Vendor != "" {
			out[i].Vendor = got.Vendor
		}
		if got.MaxAllowedSize > 0 {
			out[i].MaxAllowedSize = got.MaxAllowedSize
		}
		if got.ReasoningEffort != "" {
			out[i].ReasoningEffort = got.ReasoningEffort
		}
		if got.ReasoningSummary != "" {
			out[i].ReasoningSummary = got.ReasoningSummary
		}
		out[i].IsDefault = out[i].IsDefault || got.IsDefault
		out[i].SupportsReasoning = out[i].SupportsReasoning || got.SupportsReasoning
		out[i].SupportsToolCall = out[i].SupportsToolCall || got.SupportsToolCall
		out[i].OnlyReasoning = out[i].OnlyReasoning || got.OnlyReasoning
		out[i].SupportsImages = out[i].SupportsImages || got.SupportsImages
		out[i].CanDisableThinking = out[i].CanDisableThinking || got.CanDisableThinking
	}
	return out
}

// fetchGlobalModelsCache 探测结果缓存（语义参照 CN 侧 handler.dynamicModelsCache：1h TTL +
// 5min 失败负缓存）。按 Client 实例持有（effortsMu 同模式），测试新建 Client 即隔离。
// Mutex 内嵌，与 modelList 无并发读路径竞争（唯一读写点本文件内）。
type fetchGlobalModelsCache struct {
	sync.Mutex
	names    []string    // 成功缓存：并集模型名（已去重）；nil = 未探测/失败
	infos    []ModelInfo // 成功缓存：对象形态的全字段条目（窄表/失败形态为 nil）
	fetched  time.Time
	lastFail time.Time
}

// globalModelsTTL / globalModelsFailCooldown 探测缓存时长：成功 1h，失败 5min 负缓存。
const (
	globalModelsTTL          = time.Hour
	globalModelsFailCooldown = 5 * time.Minute
)

// globalModelsProbePaths global 企业模型目录端点候选序列（按 realm 切 base，路径"家族"）：
// /v2 家族优先（实测 /v2/enterprises/personal/models 200 含完整模型表），
// /console 作 fallback（同域旧路径，或 500）。v3-config-merge 后该家族降为企业补充路
// （/v3/config 为主路，与家族并发探测；gpt-5.3-codex 等家族独有模型经此进并集）。
var globalModelsProbePaths = []string{
	"/v2/enterprises/personal/models",
	"/console/enterprises/personal/models",
}

// FetchGlobalModels 返回 global 账号的目标模型目录。远端目录成功时更新元数据；
// 失败则回落到已验证的 19 个模型，并在 5 分钟后重试探测。
//
// 调用方负责：仅在有 global 账号时调用（无则不探测）；GlobalEnabled 关闭时（逃生门）
// 不得调用——本方法由 globalOn(a) 内部兜底，若账号因开关回落 cn 则返回 nil。
func (c *Client) FetchGlobalModels(a *auth.Auth) []string {
	names, _ := c.fetchGlobalModelsOnce(a)
	return names
}

// FetchGlobalModelInfos 探测 global 账号的模型目录并返回全字段 ModelInfo 列表。
// 与 FetchGlobalModels 共享同一次探测与缓存（names + infos 一体落缓存）：
// 对象形态 200 → 全字段条目；窄表形态 / 探测失败 / 负缓存 / 非 global 路由账号
// → nil（调用方按 ID 名单输出裸条目，不编造字段）。
// 账号因 GlobalEnabled 开关回落 cn 时不探测（globalOn 兜底，零上游调用）。
func (c *Client) FetchGlobalModelInfos(a *auth.Auth) []ModelInfo {
	_, infos := c.fetchGlobalModelsOnce(a)
	return infos
}

// fetchGlobalModelsOnce 单次探测决策（缓存命中/负缓存/触发探测），返回 (names, infos)。
// 目录始终是目标 19 模型；infos 是静态能力表，远端对象形态成功时用非空字段覆盖。
func (c *Client) fetchGlobalModelsOnce(a *auth.Auth) (names []string, infos []ModelInfo) {
	if !c.globalOn(a) {
		// 逃生门兜底：账号不路由 global 上游 → 不探测（零上游调用）。
		return nil, nil
	}

	c.globalModels.Lock()
	if len(c.globalModels.names) > 0 && c.globalModels.lastFail.IsZero() && time.Since(c.globalModels.fetched) < globalModelsTTL {
		names, infos := c.globalModels.names, c.globalModels.infos
		c.globalModels.Unlock()
		return names, infos
	}
	if !c.globalModels.lastFail.IsZero() && time.Since(c.globalModels.lastFail) < globalModelsFailCooldown {
		// 负缓存冷却期内：避免反复打上游，仍返回内置目标目录。
		names, infos := c.globalModels.names, c.globalModels.infos
		c.globalModels.Unlock()
		return names, infos
	}
	c.globalModels.Unlock()

	_, remoteInfos, efforts, defaults, err := c.probeGlobalModels(a)
	if err != nil {
		// 探测失败：负缓存，但目录保持可用。prepareBody 仍走静态 effort 兜底。
		c.globalModels.Lock()
		c.globalModels.lastFail = time.Now()
		c.globalModels.names = append([]string(nil), GlobalModelNames...)
		c.globalModels.infos = GlobalModelInfos()
		c.globalModels.fetched = time.Now()
		names, infos = c.globalModels.names, c.globalModels.infos
		c.globalModels.Unlock()
		return names, infos
	}
	// global 域 effort 能力：探测下发的 supportedEfforts/defaultEffort 权威写入 global 桶
	// （raw remote，不并入静态表——静态兜底在 prepareBody 的 globalEffortMap 与
	// /v1/models 的 EffortListing 里按需 fallback）。空探测不写（防清既有桶）。
	if len(efforts) > 0 || len(defaults) > 0 {
		c.storeEfforts("global", efforts, defaults)
	}

	c.globalModels.Lock()
	c.globalModels.names = append([]string(nil), GlobalModelNames...)
	c.globalModels.infos = overlayGlobalCatalog(remoteInfos)
	c.globalModels.fetched = time.Now()
	c.globalModels.lastFail = time.Time{}
	names, infos = c.globalModels.names, c.globalModels.infos
	c.globalModels.Unlock()
	return names, infos
}

// probeGlobalModels 发起一次 global 模型目录探测（v3-config-merge）：
// /v3/config（主，IDE UA 完整能力版）与企业端点家族（/v2 → /console 兜底，补缺）
// **并发**探测后并集合并。返回模型名列表（已合并、未再去重——去重在
// fetchGlobalModelsOnce）、全字段 ModelInfo（对象形态；窄表为 nil）及 effort
// 能力桶（supportedEfforts/defaultEffort，可为空）。合并口径：v3 条目为主
// （credits 等字段以 v3 为准），企业端点只补 v3 缺失的模型 id；去重 key =
// 模型 id，输出顺序稳定。两路全失败才返回错误（等价原「家族端点全非 2xx」
// 负缓存语义）；单路失败降级为另一路结果 + warn 日志，互不拖累。
func (c *Client) probeGlobalModels(a *auth.Auth) (names []string, infos []ModelInfo, efforts map[string][]string, defaults map[string]string, err error) {
	type probeResult struct {
		names []string
		infos []ModelInfo
		err   error
	}
	v3Ch := make(chan probeResult, 1)
	enterpriseCh := make(chan probeResult, 1)
	go func() {
		// v3 主路：复用 IDE UA 版 /v3/config 探测（chatBase 已按 realm 切 global base）。
		byID, perr := c.fetchV3ConfigModelMap(a)
		if perr != nil {
			v3Ch <- probeResult{err: perr}
			return
		}
		ids := make([]string, 0, len(byID))
		outInfos := make([]ModelInfo, 0, len(byID))
		for _, mi := range byID {
			if nonChatModel(mi.ID, mi.MaxTokens, mi.Tags) {
				continue
			}
			ids = append(ids, mi.ID)
			outInfos = append(outInfos, mi)
		}
		sort.Strings(ids) // map 迭代序随机，排序保输出稳定
		v3Ch <- probeResult{names: ids, infos: outInfos}
	}()
	go func() {
		// 企业端点家族：/v2 首选 → /console 兜底（既有探活序，零回归）。
		var lastErr error
		for _, path := range globalModelsProbePaths {
			names, infos, perr := c.globalModelsOnce(a, path)
			if perr != nil {
				lastErr = perr
				continue
			}
			enterpriseCh <- probeResult{names: names, infos: infos}
			return
		}
		enterpriseCh <- probeResult{err: lastErr}
	}()
	v3 := <-v3Ch
	enterprise := <-enterpriseCh

	if v3.err != nil && enterprise.err != nil {
		// 两路全失败 → 负缓存语义（等价原家族端点全非 2xx）。
		return nil, nil, nil, nil, v3.err
	}
	if v3.err != nil {
		// /v3 失败降级：不拖累企业端点结果（降级仅企业端点 + warn）。
		log.Printf("WARN: [upstream] global models: v3/config probe failed (degraded to enterprise endpoint): %v", v3.err)
		names, infos, efforts, defaults = extractEfforts(enterprise.infos)
		return names, infos, efforts, defaults, nil
	}
	if enterprise.err != nil {
		log.Printf("WARN: [upstream] global models: enterprise endpoint failed (v3/config only): %v", enterprise.err)
		names, infos, efforts, defaults = extractEfforts(v3.infos)
		return names, infos, efforts, defaults, nil
	}
	// 两路皆成功：v3 为主、企业端点补缺合并（含 effort 桶合并，v3 权威）。
	v3Names, v3Infos, v3Efforts, v3Defaults := extractEfforts(v3.infos)
	if len(v3Names) == 0 {
		v3Names = v3.names
	}
	entNames, entInfos, entEfforts, entDefaults := extractEfforts(enterprise.infos)
	if len(entNames) == 0 {
		entNames = enterprise.names
	}
	names, infos = mergeGlobalCatalog(v3Names, v3Infos, entNames, entInfos)
	efforts = mergeEffortBuckets(v3Efforts, entEfforts)
	defaults = mergeEffortDefaults(v3Defaults, entDefaults)
	return names, infos, efforts, defaults, nil
}

// extractEfforts 从条目列表抽取 effort 能力桶（supportedEfforts 数组优先；
// 缺数组但 defaultEffort 单档非空也入 defaults 桶）并顺带返回有序 names。
func extractEfforts(infos []ModelInfo) (names []string, out []ModelInfo, efforts map[string][]string, defaults map[string]string) {
	names = make([]string, 0, len(infos))
	for _, mi := range infos {
		if mi.ID == "" {
			continue
		}
		names = append(names, mi.ID)
		out = append(out, mi)
		if len(mi.Efforts) > 0 {
			if efforts == nil {
				efforts = make(map[string][]string)
			}
			efforts[mi.ID] = mi.Efforts
		}
		if mi.DefaultEffort != "" {
			if defaults == nil {
				defaults = make(map[string]string)
			}
			defaults[mi.ID] = mi.DefaultEffort
		}
	}
	return names, out, efforts, defaults
}

// mergeGlobalCatalog 两路合并（v3 主、企业补缺）：names 按 id 去重（v3 原序在前、
// 企业端点补充项在其原序后追加——稳定输出）；infos 同步合并（v3 条目字段权威，
// 企业端点条目只在 id 缺失时进并集）。
// 窄表形态（infos nil）时保持 nil——无对象字段不编造。
func mergeGlobalCatalog(primaryNames []string, primaryInfos []ModelInfo, secondaryNames []string, secondaryInfos []ModelInfo) (names []string, infos []ModelInfo) {
	if len(secondaryNames) == 0 {
		return primaryNames, primaryInfos
	}
	seen := make(map[string]bool, len(primaryNames)+len(secondaryNames))
	out := make([]string, 0, len(primaryNames)+len(secondaryNames))
	for _, id := range primaryNames {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	var outInfos []ModelInfo
	if primaryInfos != nil {
		outInfos = make([]ModelInfo, 0, len(primaryInfos)+len(secondaryInfos))
		outInfos = append(outInfos, primaryInfos...)
	}
	for _, id := range secondaryNames {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		// 窄表企业响应（secondaryInfos nil / 超出条目数）时该 id 无对象字段，
		// infos 保持原样（调用方按 id 名单输出裸条目，不编造字段）。
		for _, mi := range secondaryInfos {
			if mi.ID == id {
				outInfos = append(outInfos, mi)
				break
			}
		}
	}
	return out, outInfos
}

// mergeEffortBuckets 合并两路 effort 桶：主路（v3）权威，企业端点只补主路缺失的模型档位。
func mergeEffortBuckets(primary, secondary map[string][]string) map[string][]string {
	if len(secondary) == 0 {
		return primary
	}
	out := primary
	if out == nil {
		out = make(map[string][]string, len(secondary))
	}
	for id, v := range secondary {
		if _, ok := out[id]; !ok {
			out[id] = v
		}
	}
	return out
}

// mergeEffortDefaults 合并两路 defaultEffort：主路（v3）权威，企业端点只补缺失。
func mergeEffortDefaults(primary, secondary map[string]string) map[string]string {
	if len(secondary) == 0 {
		return primary
	}
	out := primary
	if out == nil {
		out = make(map[string]string, len(secondary))
	}
	for id, v := range secondary {
		if _, ok := out[id]; !ok {
			out[id] = v
		}
	}
	return out
}

// globalModelsOnce 单端点探测。2xx + 解析出非空名单 → (names, infos, nil)；否则 (nil, nil, err)。
func (c *Client) globalModelsOnce(a *auth.Auth, path string) ([]string, []ModelInfo, error) {
	url := c.chatBase(a) + path // 按 realm 切 base：global 账号 → global base
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	c.CommonHeaders(req, a) // 共享请求头（Origin/Referer/UA），与 FetchModels 同款
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		// 读失败 → 传输层错误：半截 body 不进解析（探测负缓存走 lastFail，不罚号）。
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("global models status %d: %s", resp.StatusCode, truncate(string(raw), 120))
	}
	names, infos, _, _, err := parseGlobalModelNames(raw)
	return names, infos, err
}

// parseGlobalModelNames 容忍两种形态解析模型目录：
//   - 对象数组（主形态，与 CN /console/enterprises/personal/models 同构）：data.models[]，
//     dynModelEntry 全字段（maxInputTokens/maxOutputTokens/maxAllowedSize/
//     supportsReasoning/supportsImages/reasoning.*）；id 缺省时回退 name；disabled 剔除；
//   - 窄表：data 为字符串数组 → 仅 ID，元数据留空（窗口由调用方四级查找链兜底）。
//
// 同时产出 effort 能力桶（supportedEfforts/defaultEffort）。
// 解析成功但名单为空 → 返回错误（等价"该端点没给全"）。
func parseGlobalModelNames(raw []byte) (names []string, infos []ModelInfo, efforts map[string][]string, defaults map[string]string, err error) {
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("global models parse: %w", err)
	}
	if env.Code != 0 {
		return nil, nil, nil, nil, fmt.Errorf("global models code=%d", env.Code)
	}
	trimmed := strings.TrimSpace(string(env.Data))
	if strings.HasPrefix(trimmed, "[") {
		// 窄表形态：data 为字符串数组（无 effort 元数据、无对象字段 → infos nil）。
		var arr []string
		if err := json.Unmarshal(env.Data, &arr); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("global models parse (narrow): %w", err)
		}
		out := make([]string, 0, len(arr))
		for _, id := range arr {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
		if len(out) == 0 {
			return nil, nil, nil, nil, fmt.Errorf("global models empty list")
		}
		return out, nil, nil, nil, nil
	}
	// 对象形态：data.models[]，字段名与 CN 目录一致。dynModelEntry 与 CN FetchModels
	// 共用（两域模型对象同构），零解析口径漂移。
	var obj struct {
		Models []dynModelEntry `json:"models"`
	}
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("global models parse: %w", err)
	}
	out := make([]string, 0, len(obj.Models))
	infos = make([]ModelInfo, 0, len(obj.Models))
	for _, m := range obj.Models {
		id := m.ID
		if id == "" {
			id = m.Name
		}
		if id == "" || m.Disabled {
			continue
		}
		out = append(out, id)
		mi := m.modelInfo()
		mi.ID = id // name 兜底形态下 id 取自 name，对齐 names 输出
		infos = append(infos, mi)
		// effort 桶：supportedEfforts 数组优先；缺数组但 reasoning.effort 单档非空 → 视作单档表。
		if len(m.Reasoning.SupportedEfforts) > 0 {
			if efforts == nil {
				efforts = make(map[string][]string)
			}
			efforts[id] = m.Reasoning.SupportedEfforts
		}
		if d := m.Reasoning.DefaultEffort; d != "" {
			if defaults == nil {
				defaults = make(map[string]string)
			}
			defaults[id] = d
		}
	}
	if len(out) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("global models empty list")
	}
	return out, infos, efforts, defaults, nil
}
