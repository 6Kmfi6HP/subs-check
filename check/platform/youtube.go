package platform

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
)

// youtubeReList 按优先级排列的区域提取正则,前面匹配不到才尝试后面的,
// 避免像旧版那样依赖单一正则,一旦谷歌调整页面结构就整体检测失效。
var youtubeReList = []*regexp.Regexp{
	regexp.MustCompile(`"INNERTUBE_CONTEXT_GL"\s*:\s*"([^"]+)"`),
	regexp.MustCompile(`id=["']country-code["'][^>]*>\s*([A-Za-z]{2,3})\s*<`),
	regexp.MustCompile(`"GL"\s*:\s*"([A-Za-z]{2})"`),
	regexp.MustCompile(`"countryCode"\s*:\s*"([A-Za-z]{2})"`),
	regexp.MustCompile(`"country_code"\s*:\s*"([A-Za-z]{2})"`),
}

func CheckYoutube(httpClient *http.Client) (string, error) {
	// 创建请求
	req, err := http.NewRequest("GET", "https://www.youtube.com/premium?hl=en", nil)
	if err != nil {
		return "", err
	}

	// 添加请求头
	req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9")
	req.Header.Set("sec-ch-ua", `"Chromium";v="131", "Not_A Brand";v="24", "Google Chrome";v="131"`)
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "none")
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应内容
	buf := getPooledBuf()
	defer putPooledBuf(buf)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	body := buf.Bytes()

	// 送中
	if bytes.Contains(body, []byte("www.google.cn")) {
		return "CN", nil
	}

	bodyLower := bytes.ToLower(body)

	if bytes.Contains(bodyLower, []byte("premium is not available in your country")) ||
		bytes.Contains(bodyLower, []byte("premium is not available in your region")) {
		return "", nil
	}

	// 必须状态码 2xx 且命中正向关键字才认为真正解锁,
	// 否则仅凭区域正则命中就判定解锁,遇到未开通地区的落地页也会误判。
	unlocked := resp.StatusCode >= 200 && resp.StatusCode < 300 &&
		(bytes.Contains(bodyLower, []byte("youtube premium")) ||
			bytes.Contains(bodyLower, []byte("ad-free")) ||
			bytes.Contains(bodyLower, []byte(`"browseid":"spunlimited"`)))
	if !unlocked {
		return "", nil
	}

	for _, re := range youtubeReList {
		match := re.FindSubmatch(body)
		if len(match) > 1 {
			if region := strings.ToUpper(string(match[1])); region != "" {
				return region, nil
			}
		}
	}

	return "", nil
}
