package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	disneyRegionRe    = regexp.MustCompile(`"countryCode"\s*:\s*"([^"]+)"`)
	disneySupportedRe = regexp.MustCompile(`"inSupportedLocation"\s*:\s*(false|true)`)
	disneyMainPageRe  = regexp.MustCompile(`region"\s*:\s*"([^"]+)`)
)

// DisneyResult 表示 Disney+ 检测结果
type DisneyResult struct {
	Unlocked bool   // 完全解锁
	Soon     bool   // 该地区即将上线,当前仍不可用
	Banned   bool   // IP 被 Disney+ 封禁
	Region   string // 地区码
}

// CheckDisney 检测 Disney+ 解锁状态
func CheckDisney(httpClient *http.Client) (*DisneyResult, error) {
	result := &DisneyResult{}

	// 定义常量
	const (
		cookie    = "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange&latitude=0&longitude=0&platform=browser&subject_token=DISNEYASSERTION&subject_token_type=urn%3Abamtech%3Aparams%3Aoauth%3Atoken-type%3Adevice"
		assertion = `{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}`
		authBear  = "Bearer ZGlzbmV5JmJyb3dzZXImMS4wLjA.Cu56AgSfBTDag5NiRA81oLHkDZfu5L3CKadnefEAY84"
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
	)

	// 第一步：获取 assertion token
	req, err := http.NewRequest("POST", "https://disney.api.edge.bamgrid.com/devices", strings.NewReader(assertion))
	if err != nil {
		return result, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authBear)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		result.Banned = true
		return result, nil
	}

	var assertionResp map[string]interface{}
	if err := readJSONPooled(resp.Body, &assertionResp); err != nil {
		return result, err
	}

	assertionToken, ok := assertionResp["assertion"].(string)
	if !ok {
		return result, fmt.Errorf("无法获取 assertion token")
	}

	// 第二步：获取 access token
	tokenData := strings.Replace(cookie, "DISNEYASSERTION", assertionToken, 1)
	req, err = http.NewRequest("POST", "https://disney.api.edge.bamgrid.com/token", strings.NewReader(tokenData))
	if err != nil {
		return result, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authBear)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	buf := getPooledBuf()
	defer putPooledBuf(buf)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return result, err
	}
	tokenBody := buf.Bytes()

	if bytes.Contains(tokenBody, []byte("forbidden-location")) || bytes.Contains(tokenBody, []byte("403 ERROR")) {
		result.Banned = true
		return result, nil
	}

	var tokenResp map[string]interface{}
	if err := json.Unmarshal(tokenBody, &tokenResp); err != nil {
		return result, nil
	}

	refreshToken, ok := tokenResp["refresh_token"].(string)
	if !ok {
		return result, nil
	}

	// 第三步：检查区域
	gqlQuery := fmt.Sprintf(`{"query":"mutation refreshToken($input: RefreshTokenInput!) {refreshToken(refreshToken: $input) {activeSession {sessionId}}}","variables":{"input":{"refreshToken":"%s"}}}`, refreshToken)

	req, err = http.NewRequest("POST", "https://disney.api.edge.bamgrid.com/graph/v1/device/graphql", strings.NewReader(gqlQuery))
	if err != nil {
		return result, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authBear)

	resp, err = httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	gqlBuf := getPooledBuf()
	defer putPooledBuf(gqlBuf)
	if _, err := gqlBuf.ReadFrom(resp.Body); err != nil {
		return result, err
	}
	gqlBody := gqlBuf.Bytes()

	regionMatch := disneyRegionRe.FindSubmatch(gqlBody)
	if len(regionMatch) < 2 {
		// GraphQL 响应里没有区域信息(例如接口异常/结构变化),回退去主页抓取
		return checkDisneyMainPage(httpClient, result)
	}
	region := strings.ToUpper(string(regionMatch[1]))

	// 日本地区在 GraphQL 里始终不会返回 inSupportedLocation=true,但实际已解锁,需特判
	if region == "JP" {
		result.Unlocked = true
		result.Region = region
		return result, nil
	}

	supportedMatch := disneySupportedRe.FindSubmatch(gqlBody)
	if len(supportedMatch) < 2 {
		return result, nil
	}

	if string(supportedMatch[1]) == "true" {
		result.Unlocked = true
	} else {
		result.Soon = true
	}
	result.Region = region

	return result, nil
}

// checkDisneyMainPage 在 GraphQL 检测拿不到区域信息时,回退抓取 Disney+ 主页兜底判断
func checkDisneyMainPage(httpClient *http.Client, result *DisneyResult) (*DisneyResult, error) {
	req, err := http.NewRequest("GET", "https://www.disneyplus.com/", nil)
	if err != nil {
		return result, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return result, nil
	}
	defer resp.Body.Close()

	buf := getPooledBuf()
	defer putPooledBuf(buf)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return result, nil
	}

	match := disneyMainPageRe.FindSubmatch(buf.Bytes())
	if len(match) > 1 {
		result.Unlocked = true
		result.Region = strings.ToUpper(string(match[1]))
	}

	return result, nil
}
