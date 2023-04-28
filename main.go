package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/julienschmidt/httprouter"
	"github.com/robfig/cron"
)

// define logger, sends log output to stderr
var (
	mainWg              sync.WaitGroup
	appConfig           Config
	irisClient          *IrisClient
	dbClient            *DbClient
	authChecker         *AuthChecker
	messageQueueManager *MessageQueueManager
	quit                chan int
)

func main() {

	// quit channel to signal shutdown to processes
	quit = make(chan int)

	// command line flags
	var jsonConfig string
	flag.StringVar(&jsonConfig, "config", "", "Path to a JSON config file")
	flag.Parse()

	if jsonConfig == "" {
		jsonConfig = "config/cfg.json"
	}

	// parse configs
	var err error
	if err = ParseConfig(jsonConfig, &appConfig); err != nil {
		errorlogger := log.New(os.Stderr, "iris-message-processor: ", log.Ldate|log.Ltime|log.Lshortfile)
		errorlogger.Fatalf("Failed to parse config: %s", err)
	}

	// set up logger
	logger := NewLogger("main", appConfig.LogPath, appConfig.Debug)
	defer logger.RedirectStdLogger()()
	defer logger.Close()

	// parse outbound proxy url
	if len(appConfig.OutboundProxyURL) > 0 {
		proxyURL, err := url.Parse(appConfig.OutboundProxyURL)
		if err != nil {
			// if we can't reach outbound proxy then we can't send out alerts so fail fast
			logger.Fatalf("Failed to parse outbound proxy url with error: %s", err)
		} else {
			appConfig.OutboundProxyParsedURL = proxyURL
		}
	}

	cron := cron.New()
	cron.Start()
	defer cron.Stop()
	// add a cron job to rotate logs
	err = cron.AddFunc("@daily", logger.Rotate)
	if err != nil {
		logger.Fatalf("Error adding cron job: %s", err)
	} else {
		logger.Infof("Cron job successfully added to rotate logs")
	}

	logger.Infof("iris-message-processor starting...")

	// Setup metrics collector and emit
	metricsCollector := NewMetricsCollector(MetricsCollectorConfig{
		AppName: appConfig.AppName,
		DryRun:  appConfig.MetricsDryRun,
	}, logger.Clone("metrics_collector"))
	metricsCollector.CollectRuntimeMemStats()
	metricsCollector.EmitMetrics()

	dbClient = NewDbClient(&appConfig, logger.Clone("dbClient"), &metricsCollector.Registry)

	// initialize iris-api client
	irisClient = NewIrisClient(appConfig.IrisBaseURL, appConfig.IrisAppName, appConfig.IrisKey, &metricsCollector.Registry, &appConfig, logger.Clone("irisClient"))

	// initialize authentication checker
	mainWg.Add(1)
	authChecker = NewAuthChecker(irisClient, &appConfig, logger.Clone("authChecker"), quit, &mainWg, &metricsCollector.Registry)
	go authChecker.Run()

	// initialize aggregation manager
	mainWg.Add(1)
	aggregationManager := NewAggregationManager(irisClient, &appConfig, logger.Clone("aggregationManager"), quit, &mainWg, &metricsCollector.Registry)
	go aggregationManager.Run()

	// initialize message queue manager
	mainWg.Add(1)
	messageQueueManager = NewMessageQueueManager(aggregationManager, dbClient, irisClient, &appConfig, logger.Clone("messageQueueManager"), quit, &mainWg, &metricsCollector.Registry)
	go messageQueueManager.Run()

	// initialize incident manager
	mainWg.Add(1)
	incidentManager := NewIncidentManager(irisClient, messageQueueManager, aggregationManager, &appConfig, logger.Clone("incidentManager"), quit, &mainWg, &metricsCollector.Registry)
	go incidentManager.Run()

	// define api routes
	router := httprouter.New()
	router.GET("/admin", admin)
	router.POST("/v0/notification", postNotification)
	router.GET("/v0/messages", getMessages)
	router.GET("/v0/message_status", getMessageStatus)
	router.POST("/v0/message_status", postMessageStatus)
	router.GET("/v0/message_changelogs", getMessageChangelogs)
	router.POST("/v0/message_changelog", postMessageChangelog)

	go func() {
		if appConfig.TLSCertPath != "" && appConfig.TLSKeyPath != "" {
			logger.Fatal(http.ListenAndServeTLS(":"+appConfig.Port, appConfig.TLSCertPath, appConfig.TLSKeyPath, router))
		} else {
			logger.Fatal(http.ListenAndServe(":"+appConfig.Port, nil))
		}

	}()

	osSignalChan := make(chan os.Signal, 1)
	signal.Notify(osSignalChan,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
	)
	signal.Ignore(syscall.SIGPIPE)
LOOP:
	for {
		select {
		case sig := <-osSignalChan:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				logger.Infof("Received signal %s. Shutting down gracefully.", sig)
				close(quit)
				// wait for all processes to exit gracefully
				mainWg.Wait()
				logger.Infof("Goodnight...")
				break LOOP
			case syscall.SIGUSR1:
				logger.Info("Got SIGUSR1. Rotating logs.")
				logger.Rotate()
			}
		}
	}
	logger.Info("Shutdown")

}
