// @title KILO-AI-2API
// @version 1.0.0
// @description KILO-AI-2API
// @BasePath
package main

import (
	"fmt"
	"lmarena2api/check"
	"lmarena2api/common"
	"lmarena2api/common/config"
	logger "lmarena2api/common/loggger"
	"lmarena2api/middleware"
	"lmarena2api/model"
	"lmarena2api/router"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

//var buildFS embed.FS

func main() {
	logger.SetupLogger()
	logger.SysLog(fmt.Sprintf("lmarena2api %s starting...", common.Version))

	check.CheckEnvVariable()

	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	var err error

	model.InitTokenEncoders()

	server := gin.New()
	server.Use(gin.Recovery())
	server.Use(middleware.RequestId())
	middleware.SetUpLogger(server)

	// 设置API路由
	router.SetApiRouter(server)
	// 设置前端路由
	//router.SetWebRouter(server, buildFS)

	var port = os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}

	if config.DebugEnabled {
		logger.SysLog("running in DEBUG mode.")
	}

	config.InitLACookies()

	// 打印实际生效的关键配置,便于快速定位「配了却没生效」类问题
	if config.ProxyUrl != "" {
		logger.SysLog(fmt.Sprintf("代理已启用 PROXY_URL=%s", config.ProxyUrl))
	} else {
		logger.SysLog("未配置 PROXY_URL(直连)。国内网络通常连不上 lmarena,若报 curl (28) 请在 .env 中设置代理。")
	}
	logger.SysLog(fmt.Sprintf("上游站点 LMARENA_HOST=%s", config.LmarenaBaseUrl()))
	logger.SysLog(fmt.Sprintf("已加载 cookie 数量: %d", len(config.GetLACookies())))

	logger.SysLog("lmarena2api start success. enjoy it! ^_^\n")

	//if !config.AutoRegister {
	//go job.UpdateCookieTokenTask()
	//}
	err = server.Run(":" + port)

	if err != nil {
		logger.FatalLog("failed to start HTTP server: " + err.Error())
	}
}
