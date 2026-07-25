package lmarena_api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	logger "lmarena2api/common/loggger"
	"lmarena2api/cycletls"
	"os/exec"
	"strings"
	"sync"
)

// 首先定义一个新的函数，使用curl命令执行SSE请求并返回与原函数相同类型的通道
// CurlSSE 使用curl命令执行SSE请求并返回响应通道
func CurlSSE(parentCtx context.Context, url string, options cycletls.Options) (<-chan cycletls.SSEResponse, error) {
	sseChan := make(chan cycletls.SSEResponse)

	headers := options.Headers
	data := options.Body
	cookie := ""
	if cookieVal, exists := headers["cookie"]; exists {
		cookie = cookieVal
		delete(headers, "cookie")
	}

	ctx, cancel := context.WithCancel(parentCtx)
	curlExecutionTimeoutSeconds := 300 // curl 命令自身超时时间 (秒)

	go func() {
		defer close(sseChan)
		defer cancel()

		args := []string{
			"-N",
			"--no-buffer",
			"--max-time", fmt.Sprintf("%d", curlExecutionTimeoutSeconds),
			url,
		}
		// 走代理(PROXY_URL)。此前 options.Proxy 未被传给 curl,
		// 导致国内网络下无法连接 lmarena 而报 curl (28) 超时。
		if options.Proxy != "" {
			args = append(args, "-x", options.Proxy)
		}
		for key, value := range headers {
			args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
		}
		if cookie != "" {
			args = append(args, "-b", cookie)
		}
		if data != "" {
			args = append(args, "--data-raw", data)
		}

		cmd := exec.CommandContext(ctx, "curl", args...)
		// 日志中隐去 cookie 值,避免凭据泄露
		safeArgs := make([]string, len(args))
		copy(safeArgs, args)
		for i := 0; i < len(safeArgs); i++ {
			if safeArgs[i] == "-b" && i+1 < len(safeArgs) {
				safeArgs[i+1] = "<cookie hidden>"
			}
		}
		logger.Debug(ctx, fmt.Sprintf("Executing curl for SSE: curl %s", strings.Join(safeArgs, " ")))

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			sseChan <- cycletls.SSEResponse{Status: 500, Data: fmt.Sprintf("创建stdout管道失败: %v", err), Done: true}
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			sseChan <- cycletls.SSEResponse{Status: 500, Data: fmt.Sprintf("创建stderr管道失败: %v", err), Done: true}
			return
		}

		if err := cmd.Start(); err != nil {
			sseChan <- cycletls.SSEResponse{Status: 500, Data: fmt.Sprintf("启动命令失败: %v", err), Done: true}
			return
		}

		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				select {
				case <-ctx.Done():
					return
				default:
					logger.Warnf(ctx, "curl STDERR: %s", scanner.Text())
				}
			}
		}()

		reader := bufio.NewReader(stdout)
		var lineRead string
		var readErr error

	ReadLoop:
		for {
			select {
			case <-ctx.Done():
				logger.Debug(ctx, "CurlSSE: Context done, exiting read loop.")
				break ReadLoop
			default:
				lineRead, readErr = reader.ReadString('\n')
				if readErr != nil {
					if readErr == io.EOF {
						break ReadLoop
					}
					if ctx.Err() == nil { // 仅当不是因为上下文取消导致的错误时发送
						sseChan <- cycletls.SSEResponse{Status: 500, Data: fmt.Sprintf("读取stdout出错: %v", readErr), Done: true}
					}
					return // 退出 goroutine
				}
				lineRead = strings.TrimRight(lineRead, "\r\n")
				if lineRead == "" {
					continue
				}
				sseChan <- cycletls.SSEResponse{Status: 200, Data: lineRead, Done: false}
			}
		}

		waitErr := cmd.Wait()
		if waitErr != nil {
			logger.Errorf(ctx, "CurlSSE: cmd.Wait() error: %v", waitErr)
			if ctx.Err() == nil { // 如果错误不是由父上下文取消引起的
				errMsg := fmt.Sprintf("命令执行错误: %v", waitErr)
				if exitErr, ok := waitErr.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
					errMsg = fmt.Sprintf("命令执行错误: %v, stderr: %s", waitErr, string(exitErr.Stderr))
				}
				// 避免在读取错误已发送 Done:true 后再次发送
				if readErr == io.EOF { // 仅当读取正常结束时，才将 Wait 错误视为新的终止原因
					sseChan <- cycletls.SSEResponse{Status: 500, Data: errMsg, Done: true}
				}
			}
			return // 退出 goroutine
		}

		// 如果读取循环正常结束 (EOF) 并且 cmd.Wait() 没有错误 (或者错误是由于上下文取消)
		// 并且之前没有发送过 Done:true 的错误消息
		if readErr == io.EOF && (waitErr == nil || ctx.Err() != nil) {
			sseChan <- cycletls.SSEResponse{Status: 200, Data: "[DONE]", Done: true}
		}
	}()
	return sseChan, nil
}

// ResponseCollector 用于收集响应内容
type ResponseCollector struct {
	mu      sync.Mutex
	content strings.Builder
	debug   bool // 调试模式标志
}

// AddContent 线程安全地添加内容
func (rc *ResponseCollector) AddContent(text string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.content.WriteString(text)
}

// GetContent 线程安全地获取累积的内容
func (rc *ResponseCollector) GetContent() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.content.String()
}

// LogDebug 输出调试信息
func (rc *ResponseCollector) LogDebug(format string, args ...interface{}) {
	if rc.debug {
		rc.mu.Lock()
		defer rc.mu.Unlock()
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// StreamProcessor 处理流式响应
type StreamProcessor struct {
	collector *ResponseCollector
	callback  func(content string)
	mu        sync.Mutex
}

// NewStreamProcessor 创建新的流处理器
func NewStreamProcessor(callback func(content string), debug bool) *StreamProcessor {
	return &StreamProcessor{
		collector: &ResponseCollector{debug: debug},
		callback:  callback,
	}
}

// ProcessLine 处理单行输出
func (sp *StreamProcessor) ProcessLine(line string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// 记录原始行用于调试
	sp.collector.LogDebug("收到原始行: %s", line)

	// 根据前缀处理不同类型的输出
	if strings.HasPrefix(line, "af:") {
		sp.collector.LogDebug("识别到消息头: %s", line)
		sp.collector.AddContent(line + "\n")
	} else if strings.HasPrefix(line, "a0:") {
		// 提取实际内容（去掉a0:前缀和引号）
		content := strings.Trim(line[3:], "\"")
		sp.collector.LogDebug("提取内容: %s", content)
		sp.collector.AddContent(line + "\n")
		if sp.callback != nil {
			sp.callback(content)
		}
	} else if strings.HasPrefix(line, "ae:") || strings.HasPrefix(line, "ad:") {
		sp.collector.LogDebug("识别到消息尾: %s", line)
		sp.collector.AddContent(line + "\n")
	} else {
		// 处理没有特定前缀的行
		sp.collector.LogDebug("未识别前缀的行: %s", line)
		sp.collector.AddContent(line + "\n")
	}
}

// GetCollectedContent 获取收集的内容
func (sp *StreamProcessor) GetCollectedContent() string {
	return sp.collector.GetContent()
}

// ExecuteCurlWithContext 使用上下文执行curl命令
func ExecuteCurlWithContext(ctx context.Context, url string, headers map[string]string, cookies string, data string, processor *StreamProcessor) error {
	// 构建基本命令
	args := []string{url}

	// 添加headers
	for key, value := range headers {
		args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
	}

	// 添加cookies
	if cookies != "" {
		args = append(args, "-b", cookies)
	}

	// 添加数据
	if data != "" {
		args = append(args, "--data-raw", data)
	}

	// 创建命令
	cmd := exec.CommandContext(ctx, "curl", args...)

	// 获取输出管道
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建stdout管道失败: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建stderr管道失败: %v", err)
	}

	// 启动命令
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动命令失败: %v", err)
	}

	// 创建等待组
	var wg sync.WaitGroup
	wg.Add(2) // 一个用于stdout，一个用于stderr

	// 处理标准输出
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := scanner.Text()
				processor.ProcessLine(line)
			}
		}
		if err := scanner.Err(); err != nil {
			processor.collector.LogDebug("扫描stdout出错: %v", err)
		}
	}()

	// 处理标准错误
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := scanner.Text()
				processor.collector.LogDebug("STDERR: %s", line)
			}
		}
		if err := scanner.Err(); err != nil {
			processor.collector.LogDebug("扫描stderr出错: %v", err)
		}
	}()

	// 等待所有goroutine完成
	go func() {
		wg.Wait()
	}()

	// 等待命令完成
	return cmd.Wait()
}
