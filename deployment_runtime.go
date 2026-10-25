package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/deployment"
	"eflow.local/new-api/internal/drainpool"
	"eflow.local/new-api/model"
	perfmetrics "eflow.local/new-api/pkg/perf_metrics"
)

var deploymentWorkerLock *os.File

func startDeploymentControl() *deployment.Lifecycle {
	l := &deployment.Lifecycle{
		Instance: common.NodeName, Version: common.Version, Role: common.DeploymentRole,
		Pending: drainpool.Pending, Finalize: perfmetrics.FlushAll,
		Probe: func(parent context.Context) error {
			ctx, cancel := context.WithTimeout(parent, 3*time.Second)
			defer cancel()
			primary, err := model.DB.DB()
			if err != nil {
				return err
			}
			if err = primary.PingContext(ctx); err != nil {
				return err
			}
			logs, err := model.LOG_DB.DB()
			if err != nil {
				return err
			}
			if logs != primary {
				if err = logs.PingContext(ctx); err != nil {
					return err
				}
			}
			if common.RDB == nil {
				return fmt.Errorf("Redis unavailable")
			}
			if err := common.RDB.Ping(ctx).Err(); err != nil {
				return err
			}
			if common.DeploymentRole == "web" {
				port := os.Getenv("PORT")
				if port == "" {
					port = strconv.Itoa(*common.Port)
				}
				connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
				if err != nil {
					return err
				}
				return connection.Close()
			}
			return nil
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:3001")
	if err != nil {
		common.FatalLog("deployment control listener: " + err.Error())
	}
	srv := &http.Server{Handler: l.ControlHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			common.FatalLog("deployment control server: " + err.Error())
		}
	}()
	return l
}

func waitDeploymentDrain(l *deployment.Lifecycle) {
	for {
		s := l.Snapshot()
		if s.Active == 0 && s.Pending == 0 {
			if err := perfmetrics.FlushAll(); err == nil {
				return
			}
		}
		common.SysLog(fmt.Sprintf("draining instance=%s active=%d pending=%d; keeping process alive", common.NodeName, s.Active, s.Pending))
		time.Sleep(time.Second)
	}
}
