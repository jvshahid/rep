package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nu7hatch/gouuid"
	"github.com/tedsuo/router"

	"github.com/cloudfoundry-incubator/executor/client"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"

	"github.com/cloudfoundry-incubator/rep/api"
	"github.com/cloudfoundry-incubator/rep/api/taskcomplete"
	"github.com/cloudfoundry-incubator/rep/lrp_scheduler"
	"github.com/cloudfoundry-incubator/rep/maintain"
	"github.com/cloudfoundry-incubator/rep/routes"
	"github.com/cloudfoundry-incubator/rep/task_scheduler"
)

var etcdCluster = flag.String(
	"etcdCluster",
	"http://127.0.0.1:4001",
	"comma-separated list of etcd addresses (http://ip:port)",
)

var logLevel = flag.String(
	"logLevel",
	"info",
	"the logging level (none, fatal, error, warn, info, debug, debug1, debug2, all)",
)

var syslogName = flag.String(
	"syslogName",
	"",
	"syslog name",
)

var heartbeatInterval = flag.Duration(
	"heartbeatInterval",
	60*time.Second,
	"the interval, in seconds, between heartbeats for maintaining presence",
)

var executorURL = flag.String(
	"executorURL",
	"http://127.0.0.1:1700",
	"location of executor to represent",
)

var listenAddr = flag.String(
	"listenAddr",
	"0.0.0.0:20515",
	"host:port to listen on for job completion",
)

var stack = flag.String(
	"stack",
	"",
	"the rep stack - must be specified",
)

func main() {
	flag.Parse()

	l, err := steno.GetLogLevel(*logLevel)
	if err != nil {
		log.Fatalf("Invalid loglevel: %s\n", *logLevel)
	}

	if *stack == "" {
		log.Fatalf("A stack must be specified")
	}

	stenoConfig := steno.Config{
		Level: l,
		Sinks: []steno.Sink{steno.NewIOSink(os.Stdout)},
	}

	if *syslogName != "" {
		stenoConfig.Sinks = append(stenoConfig.Sinks, steno.NewSyslogSink(*syslogName))
	}

	steno.Init(&stenoConfig)
	logger := steno.NewLogger("rep")

	etcdAdapter := etcdstoreadapter.NewETCDStoreAdapter(
		strings.Split(*etcdCluster, ","),
		workerpool.NewWorkerPool(10),
	)

	bbs := Bbs.NewRepBBS(etcdAdapter, timeprovider.NewTimeProvider())
	err = etcdAdapter.Connect()
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err,
		}, "rep.etcd-connect.failed")
		os.Exit(1)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

	executorClient := client.New(http.DefaultClient, *executorURL)

	callbackGenerator := router.NewRequestGenerator(
		"http://"+*listenAddr,
		routes.Routes,
	)

	taskRep := task_scheduler.New(callbackGenerator, bbs, logger, *stack, executorClient)
	lrpRep := lrp_scheduler.New(bbs, logger, *stack, executorClient)

	apiHandler, err := api.NewServer(taskcomplete.NewHandler(bbs, logger), nil)
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err,
		}, "rep.api-server-initialize.failed")
		os.Exit(1)
	}

	taskSchedulerReady := make(chan struct{})
	lrpSchedulerReady := make(chan struct{})
	maintainReady := make(chan struct{})

	go func() {
		<-maintainReady
		<-taskSchedulerReady
		<-lrpSchedulerReady

		fmt.Println("representative started")
	}()

	apiListener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err,
		}, "rep.listening.failed")
		os.Exit(1)
	}

	go http.Serve(apiListener, apiHandler)

	err = taskRep.Run(taskSchedulerReady)
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err,
		}, "rep.task-scheduler.failed")
		os.Exit(1)
	}

	lrpRep.Run(lrpSchedulerReady)

	////

	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}
	repID := uuid.String()

	maintainSignals := make(chan os.Signal, 1)
	signal.Notify(maintainSignals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)

	repPresence := models.RepPresence{
		RepID: repID,
		Stack: *stack,
	}
	maintainer := maintain.New(repPresence, bbs, logger, *heartbeatInterval)

	go func() {
		//keep maintaining forever, dont do anything if we fail to maintain
		for {
			err := maintainer.Run(maintainSignals, maintainReady)
			if err != nil {
				logger.Errorf("failed to start maintaining presence: %s", err.Error())
				maintainReady = make(chan struct{})
			} else {
				break
			}
		}
	}()

	for {
		sig := <-signals
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			taskRep.Stop()
			lrpRep.Stop()
			maintainSignals <- sig
			os.Exit(0)
		}
	}
}
