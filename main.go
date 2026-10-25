package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/controller"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/deployment"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/oauth"
	perfmetrics "eflow.local/new-api/pkg/perf_metrics"
	"eflow.local/new-api/plugins"
	"eflow.local/new-api/router"
	"eflow.local/new-api/service"
	"eflow.local/new-api/service/taskplugin"
	_ "eflow.local/new-api/setting/performance_setting"
	"eflow.local/new-api/setting/ratio_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	_ "net/http/pprof"
)

//go:embed web/dist
var buildFS embed.FS

//go:embed web/dist/index.html
var indexPage []byte

func main() {
	startTime := time.Now()

	err := InitResources()
	if err != nil {
		common.FatalLog("failed to initialize resources: " + err.Error())
		return
	}

	common.SysLog("New API " + common.Version + " started")
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	if common.DebugEnabled {
		common.SysLog("running in debug mode")
	}

	defer func() {
		err := model.CloseDB()
		if err != nil {
			common.FatalLog("failed to close database: " + err.Error())
		}
	}()

	if common.DeploymentRole == "migrate" {
		return
	}
	if common.RedisEnabled {
		// for compatibility with old versions
		common.MemoryCacheEnabled = true
	}
	if common.MemoryCacheEnabled {
		common.SysLog("memory cache enabled")
		common.SysLog(fmt.Sprintf("sync frequency: %d seconds", common.SyncFrequency))

		// Add panic recovery and retry for InitChannelCache
		func() {
			defer func() {
				if r := recover(); r != nil {
					if common.DeploymentRole == "web" {
						common.FatalLog("channel cache initialization failed; web role cannot repair shared configuration")
					}
					common.SysLog(fmt.Sprintf("InitChannelCache panic: %v, retrying once", r))
					// Retry once
					_, _, fixErr := model.FixAbility()
					if fixErr != nil {
						common.FatalLog(fmt.Sprintf("InitChannelCache failed: %s", fixErr.Error()))
					}
				}
			}()
			model.InitChannelCache()
		}()

		go model.SyncChannelCache(common.SyncFrequency)
	}

	// 热更新配置
	go model.SyncOptions(common.SyncFrequency)
	stopTaskPlugins := taskplugin.Default.Start(context.Background(), 30*time.Second, func(err error) {
		common.SysError("task plugin runtime refresh failed: " + err.Error())
	})
	defer stopTaskPlugins()

	if common.IsMasterNode && os.Getenv("CHANNEL_UPDATE_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_UPDATE_FREQUENCY"))
		if err != nil {
			common.FatalLog("failed to parse CHANNEL_UPDATE_FREQUENCY: " + err.Error())
		}
		go controller.AutomaticallyUpdateChannels(frequency)
	}

	go controller.AutomaticallyTestChannels()

	// Codex credential auto-refresh check every 10 minutes, refresh when expires within 1 day
	service.StartCodexCredentialAutoRefreshTask()

	// Group monitoring aggregation (Redis counter → snapshot pipeline)
	service.StartGroupMonitoringAggregation()

	// Risk control center (async rules engine + gateway block checks)
	service.StartRiskControlCenter()

	// Moderation center (async OpenAI omni-moderation; never blocks relay)
	service.StartModerationCenter()

	// Cross-instance reload of risk/moderation rules, unblocks and moderation keys
	service.StartRuntimeSync()

	// Ticket attachment orphan cleanup (master-node only)
	service.StartTicketAttachmentCleanupTask()
	service.StartUsageRefundNoticeTask()

	// Channel upstream model update check task
	controller.StartChannelUpstreamModelUpdateTask()

	if common.IsMasterNode && constant.UpdateTask {
		gopool.Go(func() {
			controller.UpdateMidjourneyTaskBulk()
		})
		gopool.Go(func() {
			controller.UpdateTaskBulk()
		})
		gopool.Go(func() {
			service.KnowledgeBatchPollingLoop(context.Background(), 15*time.Second)
		})
	}
	if os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		common.BatchUpdateEnabled = true
		common.SysLog("batch update enabled with interval " + strconv.Itoa(common.BatchUpdateInterval) + "s")
		model.InitBatchUpdater()
	}

	if os.Getenv("ENABLE_PPROF") == "true" {
		gopool.Go(func() {
			log.Println(http.ListenAndServe("0.0.0.0:8005", nil))
		})
		go common.Monitor()
		common.SysLog("pprof enabled")
	}

	err = common.StartPyroScope()
	if err != nil {
		common.SysError(fmt.Sprintf("start pyroscope error : %v", err))
	}

	var lifecycle *deployment.Lifecycle
	if common.DeploymentRole != "" {
		lifecycle = startDeploymentControl()
	}
	if common.DeploymentRole == "worker" {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		common.SysLog("worker stopping; worker upgrades require maintenance deployment")
		return
	}

	// Initialize HTTP server
	server := gin.New()
	// Keep Gin's default ForwardedByClientIP = true so deployments behind reverse
	// proxies (Nginx / Cloudflare) see real client IPs via X-Forwarded-For out of
	// the box. Admins who need stricter IP trust can enable "Trusted IP Header"
	// in risk-control settings — GetClientIP will then only honor that header.
	server.Use(gin.CustomRecovery(recoverFromPanic))
	// This will cause SSE not to work!!!
	//server.Use(gzip.Gzip(gzip.DefaultCompression))
	server.Use(middleware.RequestId())
	server.Use(middleware.PoweredBy())
	server.Use(middleware.I18n())
	middleware.SetUpLogger(server)
	// Initialize session store
	store := common.NewSessionStore(sessions.Options{
		Path:     "/",
		MaxAge:   2592000, // 30 days
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	server.Use(sessions.Sessions("session", store))

	InjectUmamiAnalytics()
	InjectGoogleAnalytics()

	// 设置路由
	router.SetRouter(server, router.WebAssets{
		BuildFS:   buildFS,
		IndexPage: indexPage,
	})
	var port = os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}

	// Log startup success message
	common.LogStartupSuccess(startTime, port)

	srv := &http.Server{Addr: ":" + port, Handler: server}
	if lifecycle != nil {
		srv.Handler = lifecycle.Wrap(server)
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.FatalLog("failed to start HTTP server: " + err.Error())
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	common.SysLog(fmt.Sprintf("received signal: %v, shutting down...", sig))
	if lifecycle != nil {
		lifecycle.Drain()
		// Rolling cleanup never imposes a deadline on existing streams/settlement.
		_ = srv.Shutdown(context.Background())
		waitDeploymentDrain(lifecycle)
		common.SysLog("drained server exited")
		return
	}

	shutdownTimeout := time.Duration(common.GetEnvOrDefault("SHUTDOWN_TIMEOUT_SECONDS", 120)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		common.SysError(fmt.Sprintf("server forced to shutdown: %v", err))
	}
	common.SysLog("server exited")
}

func InjectUmamiAnalytics() {
	analyticsInjectBuilder := &strings.Builder{}
	if os.Getenv("UMAMI_WEBSITE_ID") != "" {
		umamiSiteID := os.Getenv("UMAMI_WEBSITE_ID")
		umamiScriptURL := os.Getenv("UMAMI_SCRIPT_URL")
		if umamiScriptURL == "" {
			umamiScriptURL = "https://analytics.umami.is/script.js"
		}
		analyticsInjectBuilder.WriteString("<script defer src=\"")
		analyticsInjectBuilder.WriteString(umamiScriptURL)
		analyticsInjectBuilder.WriteString("\" data-website-id=\"")
		analyticsInjectBuilder.WriteString(umamiSiteID)
		analyticsInjectBuilder.WriteString("\"></script>")
	}
	analyticsInjectBuilder.WriteString("<!--Umami QuantumNous-->\n")
	analyticsInject := []byte(analyticsInjectBuilder.String())
	placeholder := []byte("<!--umami-->\n")
	indexPage = bytes.ReplaceAll(indexPage, placeholder, analyticsInject)
}

func InjectGoogleAnalytics() {
	analyticsInjectBuilder := &strings.Builder{}
	if os.Getenv("GOOGLE_ANALYTICS_ID") != "" {
		gaID := os.Getenv("GOOGLE_ANALYTICS_ID")
		// Google Analytics 4 (gtag.js)
		analyticsInjectBuilder.WriteString("<script async src=\"https://www.googletagmanager.com/gtag/js?id=")
		analyticsInjectBuilder.WriteString(gaID)
		analyticsInjectBuilder.WriteString("\"></script>")
		analyticsInjectBuilder.WriteString("<script>")
		analyticsInjectBuilder.WriteString("window.dataLayer = window.dataLayer || [];")
		analyticsInjectBuilder.WriteString("function gtag(){dataLayer.push(arguments);}")
		analyticsInjectBuilder.WriteString("gtag('js', new Date());")
		analyticsInjectBuilder.WriteString("gtag('config', '")
		analyticsInjectBuilder.WriteString(gaID)
		analyticsInjectBuilder.WriteString("');")
		analyticsInjectBuilder.WriteString("</script>")
	}
	analyticsInjectBuilder.WriteString("<!--Google Analytics QuantumNous-->\n")
	analyticsInject := []byte(analyticsInjectBuilder.String())
	placeholder := []byte("<!--Google Analytics-->\n")
	indexPage = bytes.ReplaceAll(indexPage, placeholder, analyticsInject)
}

func InitResources() error {
	// Initialize resources here if needed
	// This is a placeholder function for future resource initialization
	err := godotenv.Load(".env")
	if err != nil {
		if common.DebugEnabled {
			common.SysLog("No .env file found, using default environment variables. If needed, please create a .env file and set the relevant variables.")
		}
	}

	// 加载环境变量
	common.InitEnv()
	if err := common.InitDeploymentRole(); err != nil {
		return err
	}
	if common.DeploymentRole == "worker" {
		deploymentWorkerLock, err = deployment.LockWorker("/data/.rolling-worker.lock")
		if err != nil {
			return err
		}
	}

	logger.SetupLogger()

	// Initialize model settings
	ratio_setting.InitRatioSettings()

	service.InitHttpClient()

	service.InitTokenEncoders()

	// Initialize SQL Database
	err = model.InitDB()
	if err != nil {
		common.FatalLog("failed to initialize database: " + err.Error())
		return err
	}
	// Open and validate both configured servers before any schema or data
	// migration can mutate the primary database.
	err = model.InitLogDB()
	if err != nil {
		return err
	}
	err = model.MigrateDatabases()
	if err != nil {
		return err
	}

	model.CheckSetup()

	// Initialize options, should after model.InitDB()
	if err := model.InitOptionMap(); err != nil {
		return err
	}
	// eflow: 旧版访问令牌在本版本首次迁移后 30 天停用；迁移节点写入期限，其余节点只读取。
	if common.DeploymentRole == "" && common.IsMasterNode || common.DeploymentRole == "migrate" {
		if err := model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()); err != nil {
			return fmt.Errorf("legacy access token retire deadline: %w", err)
		}
	} else if err := model.LoadLegacyAccessTokenRetireAt(); err != nil {
		common.SysError("legacy access tokens are rejected until the retire deadline is migrated: " + err.Error())
	}
	if common.DeploymentRole == "" && common.IsMasterNode || common.DeploymentRole == "migrate" {
		// eflow: 私有渠道类型 58–61 → 1001–1004，失败时中止启动，维护发布不会带着半迁移的数据继续。
		happyhorseSource, sourceErr := plugins.Source("happyhorse")
		if sourceErr != nil {
			return fmt.Errorf("read happyhorse factory source: %w", sourceErr)
		}
		renumber, renumberErr := model.MigrateChannelTypeRenumberV1(fmt.Sprintf("%x", sha256.Sum256([]byte(happyhorseSource))))
		if renumberErr != nil {
			return fmt.Errorf("channel type renumber migration: %w", renumberErr)
		}
		if !renumber.AlreadyApplied {
			common.SysLog(fmt.Sprintf("channel type renumber migration applied: channels=%d tasks=%d plugin_bindings=%d",
				renumber.Channels, renumber.Tasks, renumber.PluginBindings))
		}
		if migratedModels, migrationErr := model.MigrateLegacyFlatPriceBillingModes(); migrationErr != nil {
			common.SysLog("failed to migrate legacy flat-price billing modes: " + migrationErr.Error())
		} else if len(migratedModels) > 0 {
			common.SysLog(fmt.Sprintf("migrated %d legacy flat-price billing modes: %s", len(migratedModels), strings.Join(migratedModels, ", ")))
		}

	}
	if common.DeploymentRole == "migrate" {
		return nil
	}

	// 清理旧的磁盘缓存文件
	if common.DeploymentRole == "" {
		common.CleanupOldCacheFiles()
	}

	// 初始化模型
	model.GetPricing()

	// Initialize Redis
	err = common.InitRedisClient()
	if err != nil {
		return err
	}

	perfmetrics.Init()

	// 启动系统监控
	common.StartSystemMonitor()

	// Initialize i18n
	err = i18n.Init()
	if err != nil {
		common.SysError("failed to initialize i18n: " + err.Error())
		// Don't return error, i18n is not critical
	} else {
		common.SysLog("i18n initialized with languages: " + strings.Join(i18n.SupportedLanguages(), ", "))
	}
	// Register user language loader for lazy loading
	i18n.SetUserLangLoader(model.GetUserLanguage)

	// Load custom OAuth providers from database
	err = oauth.LoadCustomProviders()
	if err != nil {
		common.SysError("failed to load custom OAuth providers: " + err.Error())
		// Don't return error, custom OAuth is not critical
	}

	return nil
}

// recoverFromPanic answers a recovered panic without echoing its details to the
// client. Once a response has started (for example an SSE stream) nothing can be
// appended safely, so the request is only logged and aborted.
func recoverFromPanic(c *gin.Context, err any) {
	common.SysLog(fmt.Sprintf("panic detected: %v", err))
	if c.Writer.Written() {
		c.Abort()
		return
	}
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "Internal server error",
			"type":    "new_api_panic",
		},
	})
}
