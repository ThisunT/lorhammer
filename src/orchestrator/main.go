package main

import (
	"flag"
	"io/ioutil"
	"lorhammer/src/model"
	"lorhammer/src/orchestrator/checker"
	"lorhammer/src/orchestrator/cli"
	"lorhammer/src/orchestrator/command"
	"lorhammer/src/orchestrator/provisioning"
	"lorhammer/src/orchestrator/testsuite"
	"lorhammer/src/tools"
	"os"
	"runtime"
	"time"

	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"lorhammer/src/orchestrator/metrics"
	"net/http"
)

var version string // set at build time
var commit string  // set at build time
var date string    // set at build time

var logger = logrus.WithField("logger", "orchestrator/main")

func main() {

	//creating the flags
	showVersion := flag.Bool("version", false, "Show current version and build time")
	port := flag.Int("port", 0, "The port to use to expose prometheus metrics, default 0 means random")
	mqttAddr := flag.String("mqtt", "", "The protocol://ip:port of mqtt")
	scenarioFromFile := flag.String("from-file", "", "A file containing a scenario to launch")
	reportFile := flag.String("report-file", "./report.json", "A file to fill reports tests in json")
	startCli := flag.Bool("cli", false, "Enter in cli mode and access to menu (stop/kill all lorhammers...)")
	flag.Parse() //flags created

	if *showVersion {  //if version flag is invoked
		logrus.WithFields(logrus.Fields{
			"version":    version,
			"commit":     commit,
			"build time": date,
			"go version": runtime.Version(),
		}).Warn("Welcome to the Lorhammer's Orchestrator")
		return
	}

	// PORT
	var httpPort int  //tcp port for prometheus
	if *port == 0 {  //for providing port to prometheus
		p, err := tools.FreeTCPPort()  //calls src/tools/whois.go to get free tcp for available on the host
		if err != nil {
			logger.WithError(err).Error("Free tcp port error")
		} else {
			logger.WithField("port", p).Info("Tcp port reserved")
			httpPort = p
		}
	} else {
		httpPort = *port
	}

	// HTTP PART
	go func() {  //http handler for prometheus
		http.Handle("/metrics", promhttp.Handler())
		logger.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil))
	}()

	// PROMETHEUS
	prometheus := metrics.NewPrometheus()  //calls orchestrator/metrics/prometheus.go/NewPrometheus fun

	// HOSTNAME
	host := "orchestrator"

	// CONSUL PART
	if *mqttAddr == "" {
		logger.Error("You need to specify at least -mqtt with protocol://ip:port")
		return
	}

	logrus.Warn("Welcome to the Lorhammer's Orchestrator")

  //creates a TestSuite structure having all the information to describe a test to execute the scenario
	var currentTestSuite testsuite.TestSuite  //orchestrator/testsuite/testsuite.go

	// MQTT PART
	mqttClient, err := tools.NewMqtt(host, *mqttAddr)   //calls src/tools/mqtt.go and creates a mqtt based on hostname and ip provided..contains a structure having client and url
	if err != nil {
		logger.WithError(err).Error("Can't build mqtt client")
	} else {
		if errMqtt := mqttClient.Connect(); errMqtt != nil {   //connecting to mqtt
			logger.WithError(errMqtt).Error("Error while connecting to mqtt")
		}
		if errHandleCmd := mqttClient.HandleCmd([]string{tools.MqttOrchestratorTopic}, func(cmd model.CMD) {  //handling the topic and msg--which is commandName and creating multiple subscriber on the same
			if errApplyCmd := command.ApplyCmd(cmd, mqttClient, func(register model.Register) error {  //orchestrator/command/in.go
				return provisioning.Provision(currentTestSuite.UUID, currentTestSuite.Provisioning, register)//Provisioning and getting the sensor to register from the scenario file //calls provision.go which further calls lorawan_server.go
			}, command.NewLorhammer); errApplyCmd != nil {   //command.NewLorhammer --//command/out.go
				logger.WithField("cmd", string(cmd.Payload)).WithError(errApplyCmd).Error("ApplyCmd error")
			}
		}); errHandleCmd != nil {
			logger.WithError(errHandleCmd).Error("Error while subscribing to topic")
		} else {
			logger.WithField("topic", tools.MqttOrchestratorTopic).Info("Listen mqtt")
		}
	}

	// SCENARIO
	if *scenarioFromFile != "" {
		configFile, err := ioutil.ReadFile(*scenarioFromFile)  //reading the scenario file
		if err != nil {
			logger.WithError(err).Panic("Error while reading test suite file")
		}
		tests, err := testsuite.FromFile(configFile) //orchestrator/testsuite/testsuite.go and returns an array having the scenario parameters
		if err != nil {
			logger.WithError(err).WithField("file", *scenarioFromFile).Panic("Error while parsing test suite file")
		}

		checkErrors := make([]checker.Error, 0)  //array of Error interface of /orchestrator/checker/checker.go
		nbErr := 0
		for _, test := range tests {
			currentTestSuite = test
			testReport, err := test.LaunchTest(mqttClient, prometheus) //orchestrator/testsuite/manager.go and has testsuite[] and returns TestReport
			if err != nil {
				logger.WithError(err).Error("Error during test")
				nbErr++
			} else if err := testReport.WriteFile(*reportFile); err != nil {
				logger.WithError(err).Error("Can't report test")
				nbErr++
			} else {
				checkErrors = append(checkErrors, testReport.ChecksError...)
			}
			time.Sleep(test.SleepAtEndTime)
		}
		os.Exit(len(checkErrors) + nbErr)
	}
	if *startCli {
		cli.Start(mqttClient)
	}
}
