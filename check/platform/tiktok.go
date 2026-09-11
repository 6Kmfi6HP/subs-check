package platform

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
)

var tiktokRegionRe = regexp.MustCompile(`"region"\s*:\s*"([a-zA-Z-]+)"`)

type tiktokStatus int

const (
	tiktokStatusFailed tiktokStatus = iota
	tiktokStatusNo
	tiktokStatusYes
)

// CheckTikTok 检测 TikTok 解锁状态
// 优先请求 cdn-cgi/trace 快速判断该 IP 是否被 Cloudflare/TikTok 直接封禁,
// 若拿不到区域码或请求失败,再回退请求首页做内容校验并提取区域。
func CheckTikTok(httpClient *http.Client) (string, error) {
	status, region := checkTikTokURL(httpClient, "https://www.tiktok.com/cdn-cgi/trace")

	if region == "" || status == tiktokStatusFailed {
		fbStatus, fbRegion := checkTikTokURL(httpClient, "https://www.tiktok.com/")
		if status != tiktokStatusNo {
			status = fbStatus
		}
		if region == "" {
			region = fbRegion
		}
	}

	if status != tiktokStatusYes || region == "" {
		return "", nil
	}
	return region, nil
}

func checkTikTokURL(httpClient *http.Client, url string) (tiktokStatus, string) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return tiktokStatusFailed, ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return tiktokStatusFailed, ""
	}
	defer resp.Body.Close()

	buf := getPooledBuf()
	defer putPooledBuf(buf)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return tiktokStatusFailed, ""
	}
	body := buf.Bytes()

	status := determineTikTokStatus(resp.StatusCode, body)

	var region string
	if matches := tiktokRegionRe.FindSubmatch(body); len(matches) > 1 {
		raw := string(matches[1])
		if code := strings.ToUpper(strings.SplitN(raw, "-", 2)[0]); code != "" {
			region = code
		}
	}

	return status, region
}

func determineTikTokStatus(statusCode int, body []byte) tiktokStatus {
	if statusCode == http.StatusForbidden || statusCode == 451 {
		return tiktokStatusNo
	}
	if statusCode < 200 || statusCode >= 300 {
		return tiktokStatusFailed
	}

	bodyLower := bytes.ToLower(body)
	if bytes.Contains(bodyLower, []byte("access denied")) ||
		bytes.Contains(bodyLower, []byte("not available in your region")) ||
		bytes.Contains(bodyLower, []byte("tiktok is not available")) {
		return tiktokStatusNo
	}
	return tiktokStatusYes
}
