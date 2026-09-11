package platform

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var netflixRe = regexp.MustCompile(`/([a-z]{2})/title/`)

// NetflixResult 表示 Netflix 检测结果
type NetflixResult struct {
	Full          bool   // 全解锁
	OriginalsOnly bool   // 仅自制剧
	Banned        bool   // IP 被 Netflix 封禁(403),区别于单纯的地区不支持
	Region        string // 地区码
}

// CheckNetflix 检测 Netflix 解锁状态
// 1. 优先请求 Fast.com 的 Netflix 测速 API,一次请求即可拿到区域,比传统 title 探测更快;
//    该接口返回 403 时说明 IP 已被 Netflix 直接封禁。
// 2. Fast.com 未给出结论时回退到传统 title 探测:
//    - 全解锁: 非自制剧title返回200/301，提取地区码 → NF-US
//    - 仅自制剧: 非自制剧title返回404，自制剧title返回200 → NF
//    - 封禁: 任一 title 返回403 → Banned
func CheckNetflix(httpClient *http.Client) (*NetflixResult, error) {
	result := &NetflixResult{}

	if region, banned := checkNetflixCDN(httpClient); banned {
		result.Banned = true
		return result, nil
	} else if region != "" {
		result.Full = true
		result.Region = region
		return result, nil
	}

	// title 81280792 是非自制剧（地区限制内容）
	// title 70143836 是自制剧（Netflix Originals）
	nonOriginalStatus := checkNetflixTitle(httpClient, "81280792")
	originalStatus := checkNetflixTitle(httpClient, "70143836")

	switch {
	case nonOriginalStatus == http.StatusForbidden || originalStatus == http.StatusForbidden:
		result.Banned = true
	case nonOriginalStatus == 200 || nonOriginalStatus == 301:
		// 非自制剧可访问 → 全解锁
		result.Full = true
		result.Region = getNetflixRegion(httpClient)
	case nonOriginalStatus == 404 && (originalStatus == 200 || originalStatus == 301):
		// 非自制剧404但自制剧可访问 → 仅自制剧
		result.OriginalsOnly = true
	}

	return result, nil
}

// checkNetflixCDN 通过 Fast.com 的 Netflix 测速 API 快速判断解锁情况。
// 返回区域码为空且 banned 为 false 时表示该接口未能给出结论,需要走传统探测。
func checkNetflixCDN(httpClient *http.Client) (region string, banned bool) {
	req, err := http.NewRequest("GET", "https://api.fast.com/netflix/speedtest/v2?https=true&token=YXNkZmFzZGxmbnNkYWZoYXNkZmhrYWxm&urlCount=1", nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return "", true
	}
	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var data struct {
		Targets []struct {
			Location struct {
				Country string `json:"country"`
			} `json:"location"`
		} `json:"targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", false
	}
	if len(data.Targets) == 0 || data.Targets[0].Location.Country == "" {
		return "", false
	}
	return strings.ToUpper(data.Targets[0].Location.Country), false
}

// checkNetflixTitle 检测指定 Netflix title 的 HTTP 状态码
func checkNetflixTitle(httpClient *http.Client, titleID string) int {
	req, err := http.NewRequest("GET", "https://www.netflix.com/title/"+titleID, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

// getNetflixRegion 通过访问特定title提取地区码
func getNetflixRegion(httpClient *http.Client) string {
	req, err := http.NewRequest("GET", "https://www.netflix.com/title/80018499", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	// 不跟随重定向，从 Location 头提取地区码
	client := *httpClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		return ""
	}

	// Location 格式如: https://www.netflix.com/xx/title/80018499
	matches := netflixRe.FindStringSubmatch(location)
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}

	return ""
}
