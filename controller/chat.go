package controller

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"io"
	"lmarena2api/common"
	"lmarena2api/common/config"
	logger "lmarena2api/common/loggger"
	"lmarena2api/cycletls"
	"lmarena2api/lmarena-api"
	"lmarena2api/model"
	"net/http"
	"strings"
	"time"
)

const (
	errServerErrMsg  = "Service Unavailable"
	responseIDFormat = "chatcmpl-%s"
)

// ChatForOpenAI @Summary OpenAI对话接口
// @Description OpenAI对话接口
// @Tags OpenAI
// @Accept json
// @Produce json
// @Param req body model.OpenAIChatCompletionRequest true "OpenAI对话请求"
// @Param Authorization header string true "Authorization API-KEY"
// @Router /v1/chat/completions [post]
func ChatForOpenAI(c *gin.Context) {
	client := cycletls.Init()
	defer safeClose(client)

	var openAIReq model.OpenAIChatCompletionRequest
	if err := c.BindJSON(&openAIReq); err != nil {
		logger.Errorf(c.Request.Context(), err.Error())
		c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: "Invalid request parameters",
				Type:    "request_error",
				Code:    "500",
			},
		})
		return
	}

	openAIReq.RemoveEmptyContentMessages()

	modelInfo, b := common.GetModelInfo(openAIReq.Model)
	if !b {
		c.JSON(http.StatusBadRequest, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: fmt.Sprintf("Model %s not supported", openAIReq.Model),
				Type:    "invalid_request_error",
				Code:    "invalid_model",
			},
		})
		return
	}

	if modelInfo.Type == "image" {
		responseId := fmt.Sprintf(responseIDFormat, time.Now().Format("20060102150405"))
		prompt := openAIReq.GetUserContent()[0]

		// Use the shared image processing function
		imageUrlList, err := processImageRequest(c, client, prompt, modelInfo)
		if err != nil {
			logger.Errorf(c.Request.Context(), "Image generation failed: %v", err)
			c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
				OpenAIError: model.OpenAIError{
					Message: err.Error(),
					Type:    "request_error",
					Code:    "500",
				},
			})
			return
		}

		var content []string
		for _, item := range imageUrlList {
			content = append(content, fmt.Sprintf("![Image](%s)", item))
		}

		if openAIReq.Stream {
			jsonData, _ := json.Marshal(prompt)
			streamResp := createStreamResponse(responseId, openAIReq.Model, jsonData, model.OpenAIDelta{Content: strings.Join(content, "\n"), Role: "assistant"}, nil)
			err := sendSSEvent(c, streamResp)
			if err != nil {
				logger.Errorf(c.Request.Context(), err.Error())
				c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
					OpenAIError: model.OpenAIError{
						Message: err.Error(),
						Type:    "request_error",
						Code:    "500",
					},
				})
				return
			}
			c.SSEvent("", " [DONE]")
			return
		} else {
			//promptTokens := common.CountTokenText(prompt, openAIReq.Model)
			//completionTokens := common.CountTokenText(strings.Join(content, "\n"), openAIReq.Model)

			finishReason := "stop"
			// Create and return OpenAIChatCompletionResponse structure
			resp := model.OpenAIChatCompletionResponse{
				ID:      responseId,
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   openAIReq.Model,
				Choices: []model.OpenAIChoice{
					{
						Message: model.OpenAIMessage{
							Role:    "assistant",
							Content: strings.Join(content, "\n"),
						},
						FinishReason: &finishReason,
					},
				},
				Usage: model.OpenAIUsage{
					//PromptTokens:     promptTokens,
					//CompletionTokens: completionTokens,
					//TotalTokens:      promptTokens + completionTokens,
				},
			}
			c.JSON(200, resp)
			return
		}
	}

	//if openAIReq.MaxTokens > modelInfo.MaxTokens {
	//    c.JSON(http.StatusBadRequest, model.OpenAIErrorResponse{
	//        OpenAIError: model.OpenAIError{
	//            Message: fmt.Sprintf("Max tokens %d exceeds limit %d", openAIReq.MaxTokens, modelInfo.MaxTokens),
	//            Type:    "invalid_request_error",
	//            Code:    "invalid_max_tokens",
	//        },
	//    })
	//    return
	//}

	if openAIReq.Stream {
		handleStreamRequest(c, client, openAIReq, modelInfo)
	} else {
		handleNonStreamRequest(c, client, openAIReq, modelInfo)
	}
}
func handleNonStreamRequest(c *gin.Context, client cycletls.CycleTLS, openAIReq model.OpenAIChatCompletionRequest, modelInfo common.ModelInfo) {
	ctx := c.Request.Context()
	cookieManager := config.NewCookieManager()
	maxRetries := len(cookieManager.Cookies)
	cookie, err := cookieManager.GetRandomCookie()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	for attempt := 0; attempt < maxRetries; attempt++ {
		requestBody, err := createRequestBody(c, &openAIReq, modelInfo, "chat")
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to marshal request body"})
			return
		}
		sseChan, err := lmarena_api.MakeStreamChatRequest(c, client, jsonData, cookie, modelInfo)
		if err != nil {
			logger.Errorf(ctx, "MakeStreamChatRequest err on attempt %d: %v", attempt+1, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		isRateLimit := false
		var delta string
		var assistantMsgContent string
		var shouldContinue bool
		thinkStartType := new(bool)
		thinkEndType := new(bool)
	SSELoop:
		for response := range sseChan {
			data := response.Data
			if data == "" {
				continue
			}
			if response.Done {
				switch {
				case common.IsUsageLimitExceeded(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie Usage limit exceeded, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					config.RemoveCookie(cookie)
					break SSELoop
				case common.IsServerError(data):
					logger.Errorf(ctx, errServerErrMsg)
					c.JSON(http.StatusInternalServerError, gin.H{"error": errServerErrMsg})
					return
				case common.IsNotLogin(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie Not Login, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					break SSELoop
				case common.IsRateLimit(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie rate limited, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					config.AddRateLimitCookie(cookie, time.Now().Add(time.Duration(config.RateLimitCookieLockDuration)*time.Second))
					break SSELoop
				}
				logger.Warnf(ctx, response.Data)
				return
			}

			logger.Debug(ctx, strings.TrimSpace(data))

			streamDelta, streamShouldContinue := processNoStreamData(c, data, modelInfo, thinkStartType, thinkEndType)
			delta = streamDelta
			shouldContinue = streamShouldContinue
			// 处理事件流数据
			if !shouldContinue {
				promptTokens := model.CountTokenText(string(jsonData), openAIReq.Model)
				completionTokens := model.CountTokenText(assistantMsgContent, openAIReq.Model)
				finishReason := "stop"

				c.JSON(http.StatusOK, model.OpenAIChatCompletionResponse{
					ID:      fmt.Sprintf(responseIDFormat, time.Now().Format("20060102150405")),
					Object:  "chat.completion",
					Created: time.Now().Unix(),
					Model:   openAIReq.Model,
					Choices: []model.OpenAIChoice{{
						Message: model.OpenAIMessage{
							Role:    "assistant",
							Content: assistantMsgContent,
						},
						FinishReason: &finishReason,
					}},
					Usage: model.OpenAIUsage{
						PromptTokens:     promptTokens,
						CompletionTokens: completionTokens,
						TotalTokens:      promptTokens + completionTokens,
					},
				})

				return
			} else {
				assistantMsgContent = assistantMsgContent + delta
			}
		}
		if !isRateLimit {
			return
		}

		// 获取下一个可用的cookie继续尝试
		cookie, err = cookieManager.GetNextCookie()
		if err != nil {
			logger.Errorf(ctx, "No more valid cookies available after attempt %d", attempt+1)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

	}
	logger.Errorf(ctx, "All cookies exhausted after %d attempts", maxRetries)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "All cookies are temporarily unavailable."})
	return
}

func createRequestBody(c *gin.Context, openAIReq *model.OpenAIChatCompletionRequest, modelInfo common.ModelInfo, chatType string) (map[string]interface{}, error) {
	client := cycletls.Init()
	defer safeClose(client)

	// 处理预设消息（如果有）
	if config.PRE_MESSAGES_JSON != "" {
		err := openAIReq.PrependMessagesFromJSON(config.PRE_MESSAGES_JSON)
		if err != nil {
			return nil, fmt.Errorf("PrependMessagesFromJSON err: %v JSON:%s", err, config.PRE_MESSAGES_JSON)
		}
	}

	// 设置最大token数
	if openAIReq.MaxTokens <= 1 {
		openAIReq.MaxTokens = 8192
	}

	// 为Arena API生成必要的UUID
	evaluationID, err := generateLowerUUID()
	if err != nil {
		return nil, fmt.Errorf("生成评估ID失败: %v", err)
	}

	// 确定使用的模型ID
	//modelID := modelInfo.ModelID
	//if modelID == "" {
	// 如果modelInfo中没有指定modelID，使用Claude 3 Opus的ID
	//modelID := "c5a11495-081a-4dc6-8d9a-64a4fd6f7bbc"
	modelID := modelInfo.ID
	//}

	// 将OpenAI消息转换为Arena API消息格式
	arenaMessages := []map[string]interface{}{}
	messageIDs := make([]string, len(openAIReq.Messages))

	// 为每条消息生成ID
	for i := range openAIReq.Messages {
		messageIDs[i], err = generateLowerUUID()
		if err != nil {
			return nil, fmt.Errorf("生成消息ID失败: %v", err)
		}
	}

	// 构建消息列表
	for i, msg := range openAIReq.Messages {
		// 处理消息内容
		var content string
		switch v := msg.Content.(type) {
		case string:
			content = v
		case []interface{}:
			// 处理多模态内容，这里简化为只提取文本部分
			textParts := []string{}
			for _, part := range v {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partMap["type"] == "text" && partMap["text"] != nil {
						if text, ok := partMap["text"].(string); ok {
							textParts = append(textParts, text)
						}
					}
				}
			}
			content = strings.Join(textParts, "\n")
		default:
			// 尝试将其他类型转换为字符串
			contentBytes, err := json.Marshal(msg.Content)
			if err != nil {
				content = fmt.Sprintf("%v", msg.Content)
			} else {
				content = string(contentBytes)
			}
		}

		// 确定父消息ID
		parentMessageIds := []string{}
		if i > 0 {
			parentMessageIds = []string{messageIDs[i-1]}
		}

		// 确定消息的modelId
		var msgModelId interface{} = nil
		if msg.Role == "assistant" {
			msgModelId = modelID
		}

		if msg.Role == "system" {
			msg.Role = "user"
		}

		// 创建Arena消息
		arenaMessage := map[string]interface{}{
			"id":                       messageIDs[i],
			"role":                     msg.Role,
			"content":                  content,
			"experimental_attachments": []interface{}{},
			"parentMessageIds":         parentMessageIds,
			"participantPosition":      "a",
			"modelId":                  msgModelId,
			"evaluationSessionId":      evaluationID,
			"status":                   "pending",
			"failureReason":            nil,
		}

		arenaMessages = append(arenaMessages, arenaMessage)
	}

	// 如果没有消息，添加一个默认的用户消息
	if len(arenaMessages) == 0 {
		userMessageID, err := generateLowerUUID()
		if err != nil {
			return nil, fmt.Errorf("生成用户消息ID失败: %v", err)
		}

		arenaMessages = append(arenaMessages, map[string]interface{}{
			"id":                       userMessageID,
			"role":                     "user",
			"content":                  "?",
			"experimental_attachments": []interface{}{},
			"parentMessageIds":         []string{},
			"participantPosition":      "a",
			"modelId":                  nil,
			"evaluationSessionId":      evaluationID,
			"status":                   "pending",
			"failureReason":            nil,
		})

		messageIDs = append(messageIDs, userMessageID)
	}

	// 添加一个空的助手消息，用于接收回复
	modelAMessageID, err := generateLowerUUID()
	if err != nil {
		return nil, fmt.Errorf("生成模型消息ID失败: %v", err)
	}

	arenaMessages = append(arenaMessages, map[string]interface{}{
		"id":                       modelAMessageID,
		"role":                     "assistant",
		"content":                  "",
		"experimental_attachments": []interface{}{},
		"parentMessageIds":         []string{messageIDs[len(messageIDs)-1]},
		"participantPosition":      "a",
		"modelId":                  modelID,
		"evaluationSessionId":      evaluationID,
		"status":                   "pending",
		"failureReason":            nil,
	})

	// 构建Arena API请求体
	arenaRequest := map[string]interface{}{
		"id":              evaluationID,
		"mode":            "direct",
		"modelAId":        modelID,
		"userMessageId":   messageIDs[len(messageIDs)-1], // 最后一条用户消息的ID
		"modelAMessageId": modelAMessageID,               // 新添加的助手消息ID
		"messages":        arenaMessages,
		"modality":        chatType,
	}

	// 记录请求体
	logger.Debug(c.Request.Context(), fmt.Sprintf("Arena API RequestBody: %v", arenaRequest))

	return arenaRequest, nil
}

// 发送请求到Arena API的函数

// 生成小写UUID的辅助函数
func generateLowerUUID() (string, error) {
	uuid := uuid.New().String()
	return strings.ToLower(uuid), nil
}

// createStreamResponse 创建流式响应
func createStreamResponse(responseId, modelName string, jsonData []byte, delta model.OpenAIDelta, finishReason *string) model.OpenAIChatCompletionResponse {
	promptTokens := model.CountTokenText(string(jsonData), modelName)
	completionTokens := model.CountTokenText(delta.Content, modelName)
	return model.OpenAIChatCompletionResponse{
		ID:      responseId,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []model.OpenAIChoice{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			},
		},
		Usage: model.OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// handleDelta 处理消息字段增量
func handleDelta(c *gin.Context, delta string, responseId, modelName string, jsonData []byte) error {
	// 创建基础响应
	createResponse := func(content string) model.OpenAIChatCompletionResponse {
		return createStreamResponse(
			responseId,
			modelName,
			jsonData,
			model.OpenAIDelta{Content: content, Role: "assistant"},
			nil,
		)
	}

	// 发送基础事件
	var err error
	if err = sendSSEvent(c, createResponse(delta)); err != nil {
		return err
	}

	return err
}

// handleMessageResult 处理消息结果
func handleMessageResult(c *gin.Context, responseId, modelName string, jsonData []byte) bool {
	finishReason := "stop"
	var delta string

	promptTokens := 0
	completionTokens := 0

	streamResp := createStreamResponse(responseId, modelName, jsonData, model.OpenAIDelta{Content: delta, Role: "assistant"}, &finishReason)
	streamResp.Usage = model.OpenAIUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}

	if err := sendSSEvent(c, streamResp); err != nil {
		logger.Warnf(c.Request.Context(), "sendSSEvent err: %v", err)
		return false
	}
	c.SSEvent("", " [DONE]")
	return false
}

// sendSSEvent 发送SSE事件
func sendSSEvent(c *gin.Context, response model.OpenAIChatCompletionResponse) error {
	jsonResp, err := json.Marshal(response)
	if err != nil {
		logger.Errorf(c.Request.Context(), "Failed to marshal response: %v", err)
		return err
	}
	c.SSEvent("", " "+string(jsonResp))
	c.Writer.Flush()
	return nil
}

func handleStreamRequest(c *gin.Context, client cycletls.CycleTLS, openAIReq model.OpenAIChatCompletionRequest, modelInfo common.ModelInfo) {

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	responseId := fmt.Sprintf(responseIDFormat, time.Now().Format("20060102150405"))
	ctx := c.Request.Context()

	cookieManager := config.NewCookieManager()
	maxRetries := len(cookieManager.Cookies)
	cookie, err := cookieManager.GetRandomCookie()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.Stream(func(w io.Writer) bool {
		for attempt := 0; attempt < maxRetries; attempt++ {
			requestBody, err := createRequestBody(c, &openAIReq, modelInfo, "chat")
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return false
			}

			jsonData, err := json.Marshal(requestBody)
			if err != nil {
				c.JSON(500, gin.H{"error": "Failed to marshal request body"})
				return false
			}
			sseChan, err := lmarena_api.MakeStreamChatRequest(c, client, jsonData, cookie, modelInfo)
			if err != nil {
				logger.Errorf(ctx, "MakeStreamChatRequest err on attempt %d: %v", attempt+1, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return false
			}

			isRateLimit := false
		SSELoop:
			for response := range sseChan {

				if response.Status == 403 {
					logger.Errorf(c, response.Data)
					//c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
					config.RemoveCookie(cookie)
					isRateLimit = true
					break SSELoop
				}

				data := response.Data
				if data == "" {
					continue
				}

				if response.Done {
					switch {
					case common.IsUsageLimitExceeded(data):
						isRateLimit = true
						logger.Warnf(ctx, "Cookie Usage limit exceeded, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
						config.RemoveCookie(cookie)
						break SSELoop
					case common.IsServerError(data):
						logger.Errorf(ctx, errServerErrMsg)
						c.JSON(http.StatusInternalServerError, gin.H{"error": errServerErrMsg})
						return false
					case common.IsNotLogin(data):
						isRateLimit = true
						logger.Warnf(ctx, "Cookie Not Login, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
						break SSELoop // 使用 label 跳出 SSE 循环
					case common.IsRateLimit(data):
						isRateLimit = true
						logger.Warnf(ctx, "Cookie rate limited, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
						config.AddRateLimitCookie(cookie, time.Now().Add(time.Duration(config.RateLimitCookieLockDuration)*time.Second))
						break SSELoop
					}
					logger.Warnf(ctx, response.Data)
					return false
				}

				logger.Debug(ctx, strings.TrimSpace(data))

				_, shouldContinue := processStreamData(c, data, responseId, openAIReq.Model, modelInfo, jsonData)
				// 处理事件流数据

				if !shouldContinue {
					return false
				}
			}

			if !isRateLimit {
				return true
			}

			// 获取下一个可用的cookie继续尝试
			cookie, err = cookieManager.GetNextCookie()
			if err != nil {
				logger.Errorf(ctx, "No more valid cookies available after attempt %d", attempt+1)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return false
			}
		}

		logger.Errorf(ctx, "All cookies exhausted after %d attempts", maxRetries)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "All cookies are temporarily unavailable."})
		return false
	})
}

// 处理流式数据的辅助函数，返回bool表示是否继续处理
func processStreamData(c *gin.Context, data, responseId, model string, modelInfo common.ModelInfo, jsonData []byte) (string, bool) {
	data = strings.TrimSpace(data)

	// Handle [DONE] marker
	if data == "[DONE]" {
		return "", false
	}

	// Parse the prefixed data format from the logs
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		// 上游返回 HTML 时给出可读原因,而不是只丢一句 Invalid data format
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(data)), "<") {
			logger.Errorf(c.Request.Context(), "上游返回非预期内容: %s | 原始片段: %.200s",
				common.DiagnoseHTMLResponse(data), data)
		} else {
			logger.Errorf(c.Request.Context(), "Invalid data format: %s", data)
		}
		return "", false
	}

	prefix := parts[0]
	content := parts[1]

	switch prefix {
	case "a0": // Text content
		// Handle actual text content
		text := content
		// Remove quotes if present
		if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
			// 解析JSON字符串，处理转义字符
			var unquotedText string
			err := json.Unmarshal([]byte(text), &unquotedText)
			if err != nil {
				logger.Errorf(c.Request.Context(), "Failed to unquote text: %v", err)
				// 如果解析失败，使用原始文本
				unquotedText = text[1 : len(text)-1]
			}
			text = unquotedText
		}

		if err := handleDelta(c, text, responseId, model, jsonData); err != nil {
			logger.Errorf(c.Request.Context(), "handleDelta err: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return "", false
		}
		return text, true

	case "af": // Message ID information
		// This appears to be metadata about the message
		return "", true

	case "ae", "ad": // End of message or completion information
		// These appear to be completion signals with usage information
		handleMessageResult(c, responseId, model, jsonData)
		return "", false

	case "cookie": // Cookie information, likely for session management
		// Process cookie if needed
		return "", true

	default:
		logger.Warnf(c.Request.Context(), "Unknown prefix in stream data: %s", prefix)
		return "", false
	}
}

func processNoStreamData(c *gin.Context, data string, modelInfo common.ModelInfo, thinkStartType *bool, thinkEndType *bool) (string, bool) {
	data = strings.TrimSpace(data)

	// Handle [DONE] marker
	if data == "[DONE]" {
		return "", false
	}

	// Parse the prefixed data format from the logs
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		// 上游返回 HTML 时给出可读原因,而不是只丢一句 Invalid data format
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(data)), "<") {
			logger.Errorf(c.Request.Context(), "上游返回非预期内容: %s | 原始片段: %.200s",
				common.DiagnoseHTMLResponse(data), data)
		} else {
			logger.Errorf(c.Request.Context(), "Invalid data format: %s", data)
		}
		return "", false
	}

	prefix := parts[0]
	content := parts[1]

	switch prefix {
	case "a0": // Text content
		// Handle actual text content
		text := content
		// Remove quotes if present
		if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
			// 解析JSON字符串，处理转义字符
			var unquotedText string
			err := json.Unmarshal([]byte(text), &unquotedText)
			if err != nil {
				logger.Errorf(c.Request.Context(), "Failed to unquote text: %v", err)
				// 如果解析失败，使用原始文本
				unquotedText = text[1 : len(text)-1]
			}
			text = unquotedText
		}

		return text, true

	case "af": // Message ID information
		// This appears to be metadata about the message
		return "", true

	case "ae", "ad": // End of message or completion information
		// These appear to be completion signals with usage information
		return "", false

	case "cookie": // Cookie information, likely for session management
		// Process cookie if needed
		return "", true

	default:
		logger.Warnf(c.Request.Context(), "Unknown prefix in stream data: %s", prefix)
		return "", false
	}

}

func processImageData(c *gin.Context, data string, modelInfo common.ModelInfo) (string, bool) {
	data = strings.TrimSpace(data)

	// Handle [DONE] marker
	if data == "[DONE]" {
		return "", false
	}

	// Parse the prefixed data format from the logs
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		// 上游返回 HTML 时给出可读原因,而不是只丢一句 Invalid data format
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(data)), "<") {
			logger.Errorf(c.Request.Context(), "上游返回非预期内容: %s | 原始片段: %.200s",
				common.DiagnoseHTMLResponse(data), data)
		} else {
			logger.Errorf(c.Request.Context(), "Invalid data format: %s", data)
		}
		return "", false
	}

	prefix := parts[0]
	content := parts[1]

	switch prefix {
	case "a0": // Text content
		// Handle actual text content
		text := content
		// Remove quotes if present
		if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
			// 解析JSON字符串，处理转义字符
			var unquotedText string
			err := json.Unmarshal([]byte(text), &unquotedText)
			if err != nil {
				logger.Errorf(c.Request.Context(), "Failed to unquote text: %v", err)
				// 如果解析失败，使用原始文本
				unquotedText = text[1 : len(text)-1]
			}
			text = unquotedText
		}

		return text, true
	case "a2":
		text := content
		// Remove quotes if present
		if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
			fmt.Println("text", text)
		}

		//var unquotedText string
		//err := json.Unmarshal([]byte(text), &unquotedText)
		//if err != nil {
		//	logger.Errorf(c.Request.Context(), "Failed to unquote text: %v", err)
		//	// 如果解析失败，使用原始文本
		//	unquotedText = text[1 : len(text)-1]
		//}
		//text = unquotedText

		var data []map[string]interface{}
		err := json.Unmarshal([]byte(text), &data)
		if err != nil {
			logger.Errorf(c.Request.Context(), "Failed to unquote text: %v", err)
			return "", false
		}

		// 检查是否有数据
		if len(data) > 0 {
			// 提取第一个元素的 image 值
			if imageURL, ok := data[0]["image"].(string); ok {
				logger.Debugf(c.Request.Context(), "Image URL: %s", imageURL)
				return imageURL, true
			} else {
				logger.Errorf(c.Request.Context(), "Invalid image data: %s", data)
			}
		} else {
			logger.Errorf(c.Request.Context(), "Invalid image data: %s", data)
		}

		return text, true

	case "af": // Message ID information
		// This appears to be metadata about the message
		return "", true

	case "ae", "ad": // End of message or completion information
		// These appear to be completion signals with usage information
		return "", false

	case "cookie": // Cookie information, likely for session management
		// Process cookie if needed
		return "", true

	default:
		logger.Warnf(c.Request.Context(), "Unknown prefix in stream data: %s", prefix)
		return "", false
	}

}

// OpenaiModels @Summary OpenAI模型列表接口
// @Description OpenAI模型列表接口
// @Tags OpenAI
// @Accept json
// @Produce json
// @Param Authorization header string true "Authorization API-KEY"
// @Success 200 {object} common.ResponseResult{data=model.OpenaiModelListResponse} "成功"
// @Router /v1/models [get]
func OpenaiModels(c *gin.Context) {
	var modelsResp []string

	modelsResp = lo.Union(common.GetModelList())

	var openaiModelListResponse model.OpenaiModelListResponse
	var openaiModelResponse []model.OpenaiModelResponse
	openaiModelListResponse.Object = "list"

	for _, modelResp := range modelsResp {
		openaiModelResponse = append(openaiModelResponse, model.OpenaiModelResponse{
			ID:     modelResp,
			Object: "model",
		})
	}
	openaiModelListResponse.Data = openaiModelResponse
	c.JSON(http.StatusOK, openaiModelListResponse)
	return
}

func safeClose(client cycletls.CycleTLS) {
	if client.ReqChan != nil {
		close(client.ReqChan)
	}
	if client.RespChan != nil {
		close(client.RespChan)
	}
}

//
//func processUrl(c *gin.Context, client cycletls.CycleTLS, chatId, cookie string, url string) (string, error) {
//	// 判断是否为URL
//	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
//		// 下载文件
//		bytes, err := fetchImageBytes(url)
//		if err != nil {
//			logger.Errorf(c.Request.Context(), fmt.Sprintf("fetchImageBytes err  %v\n", err))
//			return "", fmt.Errorf("fetchImageBytes err  %v\n", err)
//		}
//
//		base64Str := base64.StdEncoding.EncodeToString(bytes)
//
//		finalUrl, err := processBytes(c, client, chatId, cookie, base64Str)
//		if err != nil {
//			logger.Errorf(c.Request.Context(), fmt.Sprintf("processBytes err  %v\n", err))
//			return "", fmt.Errorf("processBytes err  %v\n", err)
//		}
//		return finalUrl, nil
//	} else {
//		finalUrl, err := processBytes(c, client, chatId, cookie, url)
//		if err != nil {
//			logger.Errorf(c.Request.Context(), fmt.Sprintf("processBytes err  %v\n", err))
//			return "", fmt.Errorf("processBytes err  %v\n", err)
//		}
//		return finalUrl, nil
//	}
//}
//
//func fetchImageBytes(url string) ([]byte, error) {
//	resp, err := http.Get(url)
//	if err != nil {
//		return nil, fmt.Errorf("http.Get err: %v\n", err)
//	}
//	defer resp.Body.Close()
//
//	return io.ReadAll(resp.Body)
//}
//
//func processBytes(c *gin.Context, client cycletls.CycleTLS, chatId, cookie string, base64Str string) (string, error) {
//	// 检查类型
//	fileType := common.DetectFileType(base64Str)
//	if !fileType.IsValid {
//		return "", fmt.Errorf("invalid file type %s", fileType.Extension)
//	}
//	signUrl, err := lmarena-api.GetSignURL(client, cookie, chatId, fileType.Extension)
//	if err != nil {
//		logger.Errorf(c.Request.Context(), fmt.Sprintf("GetSignURL err  %v\n", err))
//		return "", fmt.Errorf("GetSignURL err: %v\n", err)
//	}
//
//	err = lmarena-api.UploadToS3(client, signUrl, base64Str, fileType.MimeType)
//	if err != nil {
//		logger.Errorf(c.Request.Context(), fmt.Sprintf("UploadToS3 err  %v\n", err))
//		return "", err
//	}
//
//	u, err := url.Parse(signUrl)
//	if err != nil {
//		return "", err
//	}
//
//	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path), nil
//}

func ImagesForOpenAI(c *gin.Context) {
	client := cycletls.Init()
	defer safeClose(client)

	var openAIReq model.OpenAIImagesGenerationRequest
	if err := c.BindJSON(&openAIReq); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	modelInfo, b := common.GetModelInfo(openAIReq.Model)
	if !b {
		c.JSON(http.StatusBadRequest, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: fmt.Sprintf("Model %s not supported", openAIReq.Model),
				Type:    "invalid_request_error",
				Code:    "invalid_model",
			},
		})
		return
	}

	ctx := c.Request.Context()

	// 使用共享的图像处理函数
	imageUrlList, err := processImageRequest(c, client, openAIReq.Prompt, modelInfo)
	if err != nil {
		logger.Errorf(ctx, "Image generation failed: %v", err)
		c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: err.Error(),
				Type:    "request_error",
				Code:    "500",
			},
		})
		return
	}

	// 确保我们至少有一个图像URL
	if len(imageUrlList) == 0 {
		c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: "No images were generated",
				Type:    "server_error",
			},
		})
		return
	}

	// 只使用第一个生成的图像
	imageData := imageUrlList[0]

	// 创建响应
	result := &model.OpenAIImagesGenerationResponse{
		Created: time.Now().Unix(),
		Data:    make([]*model.OpenAIImagesGenerationDataResponse, 0, 1),
	}

	imageDataResp := &model.OpenAIImagesGenerationDataResponse{
		RevisedPrompt: openAIReq.Prompt,
	}

	if openAIReq.ResponseFormat == "b64_json" {
		base64Str, err := getBase64ByUrl(imageData)
		if err != nil {
			logger.Errorf(ctx, "getBase64ByUrl error: %v", err)
			c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
				OpenAIError: model.OpenAIError{
					Message: fmt.Sprintf("Failed to convert image to base64: %v", err),
					Type:    "server_error",
				},
			})
			return
		}
		imageDataResp.B64Json = "" + base64Str
	} else {
		imageDataResp.URL = imageData
	}

	result.Data = append(result.Data, imageDataResp)

	// 返回成功响应
	c.JSON(http.StatusOK, result)
}
func getBase64ByUrl(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read image data: %w", err)
	}

	// Encode the image data to Base64
	base64Str := base64.StdEncoding.EncodeToString(imgData)
	return base64Str, nil
}

// processImageRequest handles image generation requests and returns image URLs
func processImageRequest(c *gin.Context, client cycletls.CycleTLS, prompt string, modelInfo common.ModelInfo) ([]string, error) {
	ctx := c.Request.Context()
	cookieManager := config.NewCookieManager()
	maxRetries := len(cookieManager.Cookies)
	cookie, err := cookieManager.GetRandomCookie()
	if err != nil {
		return nil, err
	}

	// Create a chat completion request for image generation
	request := &model.OpenAIChatCompletionRequest{
		Messages: []model.OpenAIChatMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		requestBody, err := createRequestBody(c, request, modelInfo, "image")
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %v", err)
		}

		sseChan, err := lmarena_api.MakeStreamChatRequest(c, client, jsonData, cookie, modelInfo)
		if err != nil {
			logger.Errorf(ctx, "MakeStreamChatRequest err on attempt %d: %v", attempt+1, err)
			continue
		}

		isRateLimit := false
		var imageUrls []string

		for response := range sseChan {
			data := response.Data
			if data == "" {
				continue
			}
			if response.Done {
				switch {
				case common.IsUsageLimitExceeded(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie Usage limit exceeded, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					config.RemoveCookie(cookie)
					break
				case common.IsServerError(data):
					logger.Errorf(ctx, errServerErrMsg)
					return nil, fmt.Errorf(errServerErrMsg)
				case common.IsNotLogin(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie Not Login, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					break
				case common.IsRateLimit(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie rate limited, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					config.AddRateLimitCookie(cookie, time.Now().Add(time.Duration(config.RateLimitCookieLockDuration)*time.Second))
					break
				}
				logger.Warnf(ctx, response.Data)
				break
			}

			logger.Debug(ctx, strings.TrimSpace(data))

			imageData, ok := processImageData(c, data, modelInfo)
			if !ok {
				continue
			}

			imageUrls = append(imageUrls, imageData)
		}

		if !isRateLimit && len(imageUrls) > 0 {
			return imageUrls, nil
		}

		// Get next available cookie and continue trying
		cookie, err = cookieManager.GetNextCookie()
		if err != nil {
			logger.Errorf(ctx, "No more valid cookies available after attempt %d", attempt+1)
			return nil, err
		}
	}

	logger.Errorf(ctx, "All cookies exhausted after %d attempts", maxRetries)
	return nil, fmt.Errorf("all cookies are temporarily unavailable")
}
