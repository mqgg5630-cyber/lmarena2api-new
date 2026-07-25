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

func GetAuthToken(c *gin.Context, cookie string) (string, error) {
	cmd := exec.Command("curl", "-i", "https://canary.lmarena.ai/api/refresh",
		"-X", "POST",
		"-H", "accept: */*",
		"-H", "accept-language: zh-CN,zh;q=0.9",
		"-H", "content-length: 0",
		"-H", "content-type: application/json",
		"-b", "arena-auth-prod-v1="+cookie,
		"-H", "origin: https://canary.lmarena.ai",
		"-H", "priority: u=1, i",
		"-H", "referer: https://canary.lmarena.ai/c/81abf456-d419-456f-bd11-0fb8093fd7c9",
		"-H", "sec-ch-ua: \"Chromium\";v=\"136\", \"Google Chrome\";v=\"136\", \"Not.A/Brand\";v=\"99\"",
		"-H", "sec-ch-ua-full-version: 136.0.1613.16",
		"-H", "sec-ch-ua-full-version-list: \"Chromium\";v=\"136.0.1613.16\", \"Google Chrome\";v=\"136.0.1613.16\", \"Not.A/Brand\";v=\"99.0.0.0\"",
		"-H", "sec-ch-ua-mobile: ?0",
		"-H", "sec-ch-ua-platform: \"macOS\"",
		"-H", "sec-fetch-dest: empty",
		"-H", "sec-fetch-mode: cors",
		"-H", "sec-fetch-site: same-origin",
		"-H", "user-agent: "+config.UserAgent)

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
	headers := map[string]string{
		"accept":                      "*/*",
		"accept-language":             "zh-CN,zh;q=0.9,en;q=0.8",
		"content-type":                "text/plain;charset=UTF-8",
		"origin":                      "https://canary.lmarena.ai",
		"priority":                    "u=1, i",
		"referer":                     "https://canary.lmarena.ai/",
		"sec-ch-ua":                   "\"Microsoft Edge\";v=\"137\", \"Chromium\";v=\"137\", \"Not/A)Brand\";v=\"24\"",
		"sec-ch-ua-arch":              "\"arm\"",
		"sec-ch-ua-bitness":           "\"64\"",
		"sec-ch-ua-full-version":      "\"137.0.3296.52\"",
		"sec-ch-ua-full-version-list": "\"Microsoft Edge\";v=\"137.0.3296.52\", \"Chromium\";v=\"137.0.7151.56\", \"Not/A)Brand\";v=\"24.0.0.0\"",
		"sec-ch-ua-mobile":            "?0",
		"sec-ch-ua-model":             "\"\"",
		"sec-ch-ua-platform":          "\"macOS\"",
		"sec-ch-ua-platform-version":  "\"15.5.0\"",
		"user-agent":                  config.UserAgent,
		"cookie":                      "cf_clearance=" + config.CfClearance + ";" + "arena-auth-prod-v1=" + tokenInfo.NewCookie,
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

	logger.Debug(c.Request.Context(), fmt.Sprintf("cookie: %v", cookie))

	sseChan, err := CurlSSE(c.Request.Context(), "https://canary.lmarena.ai/api/stream/create-evaluation", options)
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
