package check

import (
	"lmarena2api/common/config"
	logger "lmarena2api/common/loggger"
)

func CheckEnvVariable() {
	logger.SysLog("environment variable checking...")

	if config.LACookie == "" {
		logger.FatalLog("环境变量 LA_COOKIE 未设置")
	}

	// CF_CLEARANCE 为可选：只有触发过 Cloudflare 人机验证的会话才会签发该 cookie，
	// 未签发时留空即可，请求将不携带该字段（携带空值反而会被判为异常）。
	if config.CfClearance == "" {
		logger.SysLog("警告: 环境变量 CF_CLEARANCE 未设置,将不携带该 cookie。若请求被 Cloudflare 拦截,请抓取后补上。")
	}

	logger.SysLog("environment variable check passed.")
}
