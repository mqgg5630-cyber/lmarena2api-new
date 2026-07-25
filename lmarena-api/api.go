package lmarena_api

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/net/http2"
	"io"
	"lmarena2api/common"
	"lmarena2api/common/config"
	logger "lmarena2api/common/loggger"
	"lmarena2api/cycletls"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
// baseURL            = "https://kilocode.ai"
// chatEndpoint       = baseURL + "/api/claude/v1/messages"
// openRouterEndpoint = baseURL + "/api/openrouter/chat/completions"
)

// buildClientHints 根据 config.UserAgent 推导 sec-ch-ua-* 系列请求头。
// 这些头必须与 User-Agent 自洽(平台/架构/版本),否则 Cloudflare 会判定为异常客户端。
func buildClientHints() map[string]string {
	ua := config.UserAgent

	platform := "\"Windows\""
	platformVersion := "\"15.0.0\""
	arch := "\"x86\""
	switch {
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		platform = "\"macOS\""
		platformVersion = "\"15.5.0\""
		if strings.Contains(ua, "Intel") {
			arch = "\"x86\""
		} else {
			arch = "\"arm\""
		}
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		platform = "\"Linux\""
		platformVersion = "\"\""
	}

	mobile := "?0"
	if strings.Contains(ua, "Mobile") {
		mobile = "?1"
	}

	// 从 UA 中取主版本号
	major := "137"
	fullVersion := "137.0.0.0"
	if m := regexp.MustCompile(`Chrome/(\d+)(\.[\d.]+)?`).FindStringSubmatch(ua); len(m) > 1 {
		major = m[1]
		fullVersion = m[1] + m[2]
		if m[2] == "" {
			fullVersion = m[1] + ".0.0.0"
		}
	}

	// Edge 与 Chrome 的品牌列表不同
	var brands, fullVersionList string
	if em := regexp.MustCompile(`Edg/(\d+)(\.[\d.]+)?`).FindStringSubmatch(ua); len(em) > 1 {
		eMajor := em[1]
		eFull := eMajor + em[2]
		if em[2] == "" {
			eFull = eMajor + ".0.0.0"
		}
		brands = fmt.Sprintf(`"Microsoft Edge";v="%s", "Chromium";v="%s", "Not/A)Brand";v="24"`, eMajor, major)
		fullVersionList = fmt.Sprintf(`"Microsoft Edge";v="%s", "Chromium";v="%s", "Not/A)Brand";v="24.0.0.0"`, eFull, fullVersion)
	} else {
		brands = fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not/A)Brand";v="24"`, major, major)
		fullVersionList = fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not/A)Brand";v="24.0.0.0"`, fullVersion, fullVersion)
	}

	return map[string]string{
		"sec-ch-ua":                   brands,
		"sec-ch-ua-arch":              arch,
		"sec-ch-ua-bitness":           "\"64\"",
		"sec-ch-ua-full-version":      "\"" + fullVersion + "\"",
		"sec-ch-ua-full-version-list": fullVersionList,
		"sec-ch-ua-mobile":            mobile,
		"sec-ch-ua-model":             "\"\"",
		"sec-ch-ua-platform":          platform,
		"sec-ch-ua-platform-version":  platformVersion,
	}
}

func GetAuthToken(c *gin.Context, cookie string) (string, error) {
	base := config.LmarenaBaseUrl()
	curlArgs := []string{"-i", base + "/api/refresh"}
	if config.ProxyUrl != "" {
		curlArgs = append(curlArgs, "-x", config.ProxyUrl)
	}
	curlArgs = append(curlArgs,
		"-X", "POST",
		"-H", "accept: */*",
		"-H", "accept-language: zh-CN,zh;q=0.9",
		"-H", "content-length: 0",
		"-H", "content-type: application/json",
		"-b", "arena-auth-prod-v1="+cookie,
		"-H", "origin: "+base,
		"-H", "priority: u=1, i",
		"-H", "referer: "+base+"/",
		"-H", "sec-ch-ua: \"Chromium\";v=\"136\", \"Google Chrome\";v=\"136\", \"Not.A/Brand\";v=\"99\"",
		"-H", "sec-ch-ua-full-version: 136.0.1613.16",
		"-H", "sec-ch-ua-full-version-list: \"Chromium\";v=\"136.0.1613.16\", \"Google Chrome\";v=\"136.0.1613.16\", \"Not.A/Brand\";v=\"99.0.0.0\"",
		"-H", "sec-ch-ua-mobile: ?0",
		"-H", "sec-ch-ua-platform: \"macOS\"",
		"-H", "sec-fetch-dest: empty",
		"-H", "sec-fetch-mode: cors",
		"-H", "sec-fetch-site: same-origin",
		"-H", "user-agent: "+config.UserAgent)

	cmd := exec.Command("curl", curlArgs...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("执行curl命令失败: %v", err)
	}

	response := string(output)
	re := regexp.MustCompile(`set-cookie: arena-auth-prod-v1=([^;]+)`)
	matches := re.FindStringSubmatch(response)

	if len(matches) > 1 {
		return matches[1], nil
	}
	//logger.SysError(fmt.Sprintf("output: %v", output))
	return "", fmt.Errorf("未找到arena-auth-prod-v1 cookie")
}

func MakeStreamChatRequest(c *gin.Context, client cycletls.CycleTLS, jsonData []byte, cookie string, modelInfo common.ModelInfo) (<-chan cycletls.SSEResponse, error) {
	tokenInfo, ok := config.LATokenMap[cookie]
	if !ok {
		return nil, fmt.Errorf("cookie not found in ASTokenMap")
	}

	// CF_CLEARANCE 未配置时不要拼出 "cf_clearance=;" 这样的空值字段,
	// 空值 cookie 反而更容易被 Cloudflare 判定为异常请求。
	cookieHeader := "arena-auth-prod-v1=" + tokenInfo.NewCookie
	if config.CfClearance != "" {
		cookieHeader = "cf_clearance=" + config.CfClearance + ";" + cookieHeader
	}

	headers := map[string]string{
		"accept":                      "*/*",
		"accept-language":             "zh-CN,zh;q=0.9,en;q=0.8",
		"content-type":                "text/plain;charset=UTF-8",
		"origin":                      config.LmarenaBaseUrl(),
		"priority":                    "u=1, i",
		"referer":                     config.LmarenaBaseUrl() + "/",
		"user-agent":                  config.UserAgent,
		"cookie":                      cookieHeader,
	}
	for k, v := range buildClientHints() {
		headers[k] = v
	}

	options := cycletls.Options{
		Ja3:        "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,0-23-65281-10-11-35-16-5-13-18-51-45-43-27-17513-21,29-23-24,0",
		UserAgent:  config.UserAgent,
		Timeout:    10 * 60 * 60,
		Proxy:      config.ProxyUrl, // 在每个请求中设置代理
		Body:       string(jsonData),
		Method:     "POST",
		Headers:    headers,
		ForceHTTP1: false,
	}

	// 不要输出完整 cookie: 它是可直接冒用身份的凭据(JWT 内含邮箱等信息)
	logger.Debug(c.Request.Context(), fmt.Sprintf("cookie: %s", common.MaskSecret(cookie)))

	sseChan, err := CurlSSE(c.Request.Context(), config.LmarenaBaseUrl()+"/api/stream/create-evaluation", options)
	if err != nil {
		logger.Errorf(c, "Failed to make stream request: %v", err)
		return nil, fmt.Errorf("Failed to make stream request: %v", err)
	}
	return sseChan, nil
}

// DoSSEWithHTTP2 返回与cycletls.DoSSEWithHTTP2相同类型的通道
func DoSSEWithHTTP2(ctx context.Context, endPoint string, method string, headers map[string]string, body string, proxyURL string) (<-chan cycletls.SSEResponse, error) {
	// 创建SSE响应通道
	sseChan := make(chan cycletls.SSEResponse, 100)

	// 创建一个支持HTTP/2的Transport
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false, // 生产环境应设为false
		},
	}

	// 设置代理（如果提供）
	if proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %v", err)
		}
		transport.Proxy = http.ProxyURL(parsedProxy)
	}

	// 显式启用HTTP/2
	err := http2.ConfigureTransport(transport)
	if err != nil {
		return nil, fmt.Errorf("failed to configure HTTP/2: %v", err)
	}

	// 创建HTTP客户端
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(10*60) * time.Second, // 10分钟超时
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, method, endPoint, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// 设置请求头
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// 确保Content-Type已设置（如果未提供且不是GET请求）
	if req.Header.Get("Content-Type") == "" && method != "GET" {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}

	// 生成请求ID
	requestID := uuid.New().String()

	// 启动goroutine处理SSE响应
	go func() {
		defer close(sseChan)
		defer client.CloseIdleConnections()

		// 发送请求
		resp, err := client.Do(req)
		if err != nil {
			sseChan <- cycletls.SSEResponse{
				RequestID: requestID,
				Status:    0,
				Data:      fmt.Sprintf("Error: %v", err),
				Done:      true,
				FinalUrl:  endPoint,
			}
			return
		}
		defer resp.Body.Close()

		// 检查响应状态
		if resp.StatusCode != http.StatusOK {
			// 读取错误响应体
			bodyBytes, _ := io.ReadAll(resp.Body)
			sseChan <- cycletls.SSEResponse{
				RequestID: requestID,
				Status:    resp.StatusCode,
				Data:      string(bodyBytes),
				Done:      true,
				FinalUrl:  resp.Request.URL.String(),
			}
			return
		}

		// 处理SSE流
		reader := bufio.NewReader(resp.Body)

		for {
			select {
			case <-ctx.Done():
				// 上下文已取消，退出处理
				sseChan <- cycletls.SSEResponse{
					RequestID: requestID,
					Status:    resp.StatusCode,
					Data:      fmt.Sprintf("Context canceled: %v", ctx.Err()),
					Done:      true,
					FinalUrl:  resp.Request.URL.String(),
				}
				return
			default:
				// 读取一行
				line, err := reader.ReadString('\n')
				if err != nil {
					if err != io.EOF {
						sseChan <- cycletls.SSEResponse{
							RequestID: requestID,
							Status:    resp.StatusCode,
							Data:      fmt.Sprintf("Error reading SSE: %v", err),
							Done:      true,
							FinalUrl:  resp.Request.URL.String(),
						}
					} else {
						// EOF表示流结束
						sseChan <- cycletls.SSEResponse{
							RequestID: requestID,
							Status:    resp.StatusCode,
							Data:      "",
							Done:      true,
							FinalUrl:  resp.Request.URL.String(),
						}
					}
					return
				}

				// 处理SSE行
				line = strings.TrimSpace(line)

				if line == "" {
					// 空行继续读取
					continue
				}

				//if strings.HasPrefix(line, "data: ") {
				// 提取数据部分
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					// 流结束
					sseChan <- cycletls.SSEResponse{
						RequestID: requestID,
						Status:    resp.StatusCode,
						Data:      "[DONE]",
						Done:      true,
						FinalUrl:  resp.Request.URL.String(),
					}
					return
				}

				// 发送数据
				sseChan <- cycletls.SSEResponse{
					RequestID: requestID,
					Status:    resp.StatusCode,
					Data:      data,
					Done:      false,
					FinalUrl:  resp.Request.URL.String(),
				}
				//}
			}
		}
	}()

	return sseChan, nil
}

func MakeSignUpRequest(token string, cfClearance string) (string, error) {
	// 构建请求数据
	requestData := fmt.Sprintf(`{"turnstile_token":"%s"}`, token)

	// 构建curl命令
	cmd := exec.Command("curl",
		//"-x", "http://206.237.11.11:52344",
		"https://canary.lmarena.ai/api/sign-up",
		"-H", "accept: */*",
		"-H", "accept-language: en-US,en;q=0.9",
		"-H", "cookie: "+cfClearance,
		"-H", "content-type: text/plain;charset=UTF-8",
		"-H", "origin: https://canary.lmarena.ai",
		"-H", "priority: u=1, i",
		"-H", "referer: https://canary.lmarena.ai/",
		"-H", "sec-ch-ua: \"Google Chrome\";v=\"135\", \"Not-A.Brand\";v=\"8\", \"Chromium\";v=\"135\"",
		"-H", "sec-ch-ua-mobile: ?0",
		"-H", "sec-ch-ua-platform: \"macOS\"",
		"-H", "sec-fetch-dest: empty",
		"-H", "sec-fetch-mode: cors",
		"-H", "sec-fetch-site: same-origin",
		"-H", "user-agent: "+config.UserAgent,
		"--data-raw", requestData,
		"-s") // 添加-s参数使curl静默输出，不显示进度信息

	// 执行命令并获取输出
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("执行curl命令失败: %v, 输出: %s", err, string(output))
	}

	// 提取JSON部分（假设响应是一个完整的JSON对象）
	jsonStr := string(output)

	// 检查是否为有效的JSON
	var jsonObj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonObj); err != nil {
		return "", fmt.Errorf("解析JSON失败: %v", err)
	}

	// 将JSON转换为Base64
	base64Str := base64.StdEncoding.EncodeToString([]byte(jsonStr))

	return base64Str, nil
}
