package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"lmarena2api/common/env"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var BackendSecret = os.Getenv("BACKEND_SECRET")
var CfClearance = os.Getenv("CF_CLEARANCE")
var AutoRegister = env.Bool("AUTO_REGISTER", false)
var LACookie = os.Getenv("LA_COOKIE")
var MysqlDsn = os.Getenv("MYSQL_DSN")
var IpBlackList = strings.Split(os.Getenv("IP_BLACK_LIST"), ",")
var DebugSQLEnabled = strings.ToLower(os.Getenv("DEBUG_SQL")) == "true"
var ProxyUrl = env.String("PROXY_URL", "")
var UserAgent = env.String("USER_AGENT", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
var CheatEnabled = env.Bool("CHEAT_ENABLED", false)
var CheatUrl = env.String("CHEAT_URL", "https://kl.goeast.io/kilo/cheat")
var ChatMaxDays = env.Int("CHAT_MAX_DAYS", -1)
var ApiSecret = os.Getenv("API_SECRET")
var ApiSecrets = strings.Split(os.Getenv("API_SECRET"), ",")

var RateLimitCookieLockDuration = env.Int("RATE_LIMIT_COOKIE_LOCK_DURATION", 10*60)

// 隐藏思考过程
var ReasoningHide = env.Int("REASONING_HIDE", 0)

// 前置message
var PRE_MESSAGES_JSON = env.String("PRE_MESSAGES_JSON", "")

// 路由前缀
var RoutePrefix = env.String("ROUTE_PREFIX", "")
var SwaggerEnable = os.Getenv("SWAGGER_ENABLE")
var BackendApiEnable = env.Int("BACKEND_API_ENABLE", 1)

var DebugEnabled = os.Getenv("DEBUG") == "true"

var RateLimitKeyExpirationDuration = 20 * time.Minute

var RequestOutTimeDuration = 5 * time.Minute

var (
	RequestRateLimitNum            = env.Int("REQUEST_RATE_LIMIT", 60)
	RequestRateLimitDuration int64 = 1 * 60
)

type RateLimitCookie struct {
	ExpirationTime time.Time // 过期时间
}

var (
	rateLimitCookies sync.Map // 使用 sync.Map 管理限速 Cookie
)

func AddRateLimitCookie(cookie string, expirationTime time.Time) {
	rateLimitCookies.Store(cookie, RateLimitCookie{
		ExpirationTime: expirationTime,
	})
	//fmt.Printf("Storing cookie: %s with value: %+v\n", cookie, RateLimitCookie{ExpirationTime: expirationTime})
}

type LATokenInfo struct {
	NewCookie string
}

var (
	LATokenMap   = map[string]LATokenInfo{}
	LACookies    []string   // 存储所有的 cookies
	cookiesMutex sync.Mutex // 保护 LACookies 的互斥锁
)

func InitLACookies() {
	cookiesMutex.Lock()
	defer cookiesMutex.Unlock()

	LACookies = []string{}

	cookieStr := os.Getenv("LA_COOKIE")
	//if AutoRegister {
	/*task, err := yescaptcha.CreateTask()
	if err != nil {
		fmt.Println("创建任务失败:", err)
		return
	}
	token, err := yescaptcha.GetTaskResult(task)
	if err != nil {
		fmt.Println("获取结果失败:", err)
		return
	}
	resp, err := MakeSignUpRequest(token)
	if err != nil {
		fmt.Println("API请求失败:", err)
		return
	}*/
	//cookieStr := "base64-" + "eyJhY2Nlc3NfdG9rZW4iOiJleUpoYkdjaU9pSklVekkxTmlJc0ltdHBaQ0k2SWtOVFQwNHhkM05uU0hkRlNFTkNNbGNpTENKMGVYQWlPaUpLVjFRaWZRLmV5SnBjM01pT2lKb2RIUndjem92TDJoMWIyZDZiMlZ4ZW1OeVpIWnJkM1IyYjJScExuTjFjR0ZpWVhObExtTnZMMkYxZEdndmRqRWlMQ0p6ZFdJaU9pSTVOekEyT1dGa055MDFPRGcxTFRRd056VXRZamhrT0MxalptSXdaak16Wm1KaE5qVWlMQ0poZFdRaU9pSmhkWFJvWlc1MGFXTmhkR1ZrSWl3aVpYaHdJam94TnpRM056UXdNVFUzTENKcFlYUWlPakUzTkRjM016WTFOVGNzSW1WdFlXbHNJam9pSWl3aWNHaHZibVVpT2lJaUxDSmhjSEJmYldWMFlXUmhkR0VpT250OUxDSjFjMlZ5WDIxbGRHRmtZWFJoSWpwN0ltbGtJam9pTmpRMU1qa3hOR010TWpKaE15MDBPREF6TFdJd1lqUXRPRGd6T1RBNE56aG1ZVGRrSW4wc0luSnZiR1VpT2lKaGRYUm9aVzUwYVdOaGRHVmtJaXdpWVdGc0lqb2lZV0ZzTVNJc0ltRnRjaUk2VzNzaWJXVjBhRzlrSWpvaVlXNXZibmx0YjNWeklpd2lkR2x0WlhOMFlXMXdJam94TnpRM05qWXlNREEyZlYwc0luTmxjM05wYjI1ZmFXUWlPaUkzTWpWbU9EazJOQzAyTjJFNExUUm1NREl0T0RoaU9DMDFPREptTkRVeE56VTBOV1VpTENKcGMxOWhibTl1ZVcxdmRYTWlPblJ5ZFdWOS43SmZzSm1ETm5ibTZ6ckE0ckRjM2RSdXhnbWRCcENUVGx6WHNmcnpJeGEwIiwidG9rZW5fdHlwZSI6ImJlYXJlciIsImV4cGlyZXNfaW4iOjM2MDAsImV4cGlyZXNfYXQiOjE3MzA0MjAwMDAsInJlZnJlc2hfdG9rZW4iOiIyaWlueTdlNXE0Zm4iLCJ1c2VyIjp7ImlkIjoiOTcwNjlhZDctNTg4NS00MDc1LWI4ZDgtY2ZiMGYzM2ZiYTY1IiwiYXVkIjoiYXV0aGVudGljYXRlZCIsInJvbGUiOiJhdXRoZW50aWNhdGVkIiwiZW1haWwiOiIiLCJwaG9uZSI6IiIsImxhc3Rfc2lnbl9pbl9hdCI6IjIwMjUtMDUtMTlUMTM6NDA6MDYuNjcxNDU3WiIsImFwcF9tZXRhZGF0YSI6e30sInVzZXJfbWV0YWRhdGEiOnsiaWQiOiI2NDUyOTE0Yy0yMmEzLTQ4MDMtYjBiNC04ODM5MDg3OGZhN2QifSwiaWRlbnRpdGllcyI6W10sImNyZWF0ZWRfYXQiOiIyMDI1LTA1LTE5VDEzOjQwOjA2LjY2OTg5WiIsInVwZGF0ZWRfYXQiOiIyMDI1LTA1LTIwVDEwOjIyOjM3LjA5MjM2MVoiLCJpc19hbm9ueW1vdXMiOnRydWV9fQ=="
	//}
	// 从环境变量中读取 LA_COOKIE 并拆分为切片
	//if cookieStr != "" {
	//
	for _, cookie := range strings.Split(cookieStr, ",") {
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			continue
		}
		LACookies = append(LACookies, cookie)
		LATokenMap[cookie] = LATokenInfo{
			NewCookie: cookie,
			// 其他字段如果需要的话也可以设置
		}
	}
	//}
}

type CookieManager struct {
	Cookies      []string
	currentIndex int
	mu           sync.Mutex
}

// GetLACookies 获取 LACookies 的副本
func GetLACookies() []string {
	//cookiesMutex.Lock()
	//defer cookiesMutex.Unlock()

	// 返回 LACookies 的副本，避免外部直接修改
	cookiesCopy := make([]string, len(LACookies))
	copy(cookiesCopy, LACookies)
	return cookiesCopy
}

func NewCookieManager() *CookieManager {
	var validCookies []string
	// 遍历 LACookies
	for _, cookie := range GetLACookies() {
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			continue // 忽略空字符串
		}

		// 检查是否在 RateLimitCookies 中
		if value, ok := rateLimitCookies.Load(cookie); ok {
			rateLimitCookie, ok := value.(RateLimitCookie) // 正确转换为 RateLimitCookie
			if !ok {
				continue
			}
			if rateLimitCookie.ExpirationTime.After(time.Now()) {
				// 如果未过期，忽略该 cookie
				continue
			} else {
				// 如果已过期，从 RateLimitCookies 中删除
				rateLimitCookies.Delete(cookie)
			}
		}

		// 添加到有效 cookie 列表
		validCookies = append(validCookies, cookie)
	}

	return &CookieManager{
		Cookies:      validCookies,
		currentIndex: 0,
	}
}

func (cm *CookieManager) GetRandomCookie() (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(cm.Cookies) == 0 {
		return "", errors.New("no cookies available")
	}

	// 生成随机索引
	randomIndex := rand.Intn(len(cm.Cookies))
	// 更新当前索引
	cm.currentIndex = randomIndex

	return cm.Cookies[randomIndex], nil
}

func (cm *CookieManager) GetNextCookie() (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(cm.Cookies) == 0 {
		return "", errors.New("no cookies available")
	}

	cm.currentIndex = (cm.currentIndex + 1) % len(cm.Cookies)
	return cm.Cookies[cm.currentIndex], nil
}

// RemoveCookie 删除指定的 cookie（支持并发）
func RemoveCookie(cookieToRemove string) {
	cookiesMutex.Lock()
	defer cookiesMutex.Unlock()

	// 创建一个新的切片，过滤掉需要删除的 cookie
	var newCookies []string
	for _, cookie := range GetLACookies() {
		if cookie != cookieToRemove {
			newCookies = append(newCookies, cookie)
		}
	}

	// 更新 GSCookies
	LACookies = newCookies
}

func MakeSignUpRequest(token string) (string, error) {
	// 构建请求数据
	requestData := fmt.Sprintf(`{"turnstile_token":"%s"}`, token)

	// 构建curl命令
	cmd := exec.Command("curl",
		"https://beta.lmarena.ai/api/sign-up",
		"-H", "accept: */*",
		"-H", "accept-language: en-US,en;q=0.9",
		"-H", "cookie: cf_clearance=20VPnkBAX4ekFnvhZAC6JJMR27HmPUqbjqTrj_N5RYE-1747743176-1.2.1.1-QC67qVWttXKkZs3RaGtGRs4xgzylmNjJFa2tbq2ZPqDqsmVQUPom4lB.vkDCImUwjzCKQi93eteDYgPaU7ntpnrVW08e3rQVJlpu42HWambeMrLa7.YRjhddbx8o5Fjq6NJ2tqBI_kiiCbB_r5kAEe_mjmFbhc6w46QLdwcLdKl4GyMTGektNXpKabYPWhCIB40wZf31cWyzq6akRGSoCIRiHP8UvDHkaTJnGNDBbA4uGU8zZC6gYT.kw7D_MLhpBLjZgGhEnONQMmr0L.Ci_XGEltfj8HbJUtwuFqSjvXD3H7ZmBYWMICImqtjNN28jbFhllGBLElhxHDaSPPF3MB5YtFPUvGIerqQAbRxAzk_VKGCGnsYiBFm7zlcur5pi;",
		"-H", "content-type: text/plain;charset=UTF-8",
		"-H", "origin: https://beta.lmarena.ai",
		"-H", "priority: u=1, i",
		"-H", "referer: https://beta.lmarena.ai/",
		"-H", "sec-ch-ua: \"Google Chrome\";v=\"135\", \"Not-A.Brand\";v=\"8\", \"Chromium\";v=\"135\"",
		"-H", "sec-ch-ua-mobile: ?0",
		"-H", "sec-ch-ua-platform: \"macOS\"",
		"-H", "sec-fetch-dest: empty",
		"-H", "sec-fetch-mode: cors",
		"-H", "sec-fetch-site: same-origin",
		"-H", "user-agent: "+UserAgent,
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
