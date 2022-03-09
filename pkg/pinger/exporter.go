package pinger

import (
	"os"
	"sync"
	"time"

	"github.com/greenpau/ovsdb"
	"k8s.io/klog/v2"
)

const metricNamespace = "kube_ovn"

var (
	appName       = "ovs-monitor"
	tryConnectCnt = 0
)

// Exporter collects OVS data from the given server and exports them using
// the prometheus metrics package.
type Exporter struct {
	sync.RWMutex
	Client       *ovsdb.OvsClient
	timeout      int
	pollInterval int
	errors       int64
	errorsLocker sync.RWMutex
}

// NewExporter returns an initialized Exporter.
func NewExporter(cfg *Configuration) *Exporter {
	e := Exporter{}
	e.Client = ovsdb.NewOvsClient()
	e.initParas(cfg)

	if err := e.Client.GetSystemID(); err != nil {
		klog.Errorf("%s failed to get system id: %s", appName, err)
	}
	if err := e.StartConnection(); err != nil {
		klog.Errorf("%s failed to connect db properly: %s", appName, err)
		go e.tryClientConnection()
	}

	return &e
}

func (e *Exporter) initParas(cfg *Configuration) {
	e.timeout = cfg.PollTimeout
	e.pollInterval = cfg.PollInterval

	e.Client.Timeout = cfg.PollTimeout
	e.Client.System.RunDir = cfg.SystemRunDir
	e.Client.Database.Vswitch.Name = cfg.DatabaseVswitchName
	e.Client.Database.Vswitch.Socket.Remote = cfg.DatabaseVswitchSocketRemote
	e.Client.Database.Vswitch.File.Data.Path = cfg.DatabaseVswitchFileDataPath
	e.Client.Database.Vswitch.File.Log.Path = cfg.DatabaseVswitchFileLogPath
	e.Client.Database.Vswitch.File.Pid.Path = cfg.DatabaseVswitchFilePidPath
	e.Client.Database.Vswitch.File.SystemID.Path = cfg.DatabaseVswitchFileSystemIDPath

	e.Client.Service.Vswitchd.File.Log.Path = cfg.ServiceVswitchdFileLogPath
	e.Client.Service.Vswitchd.File.Pid.Path = cfg.ServiceVswitchdFilePidPath
	e.Client.Service.OvnController.File.Log.Path = cfg.ServiceOvnControllerFileLogPath
	e.Client.Service.OvnController.File.Pid.Path = cfg.ServiceOvnControllerFilePidPath
}

// StartConnection connect to database socket
func (e *Exporter) StartConnection() error {
	if err := e.Client.Connect(); err != nil {
		return err
	}

	if err := e.Client.GetSystemInfo(); err != nil {
		return err
	}
	klog.Infof("%s: exporter connect successfully", e.Client.System.Hostname)

	return nil
}

func (e *Exporter) tryClientConnection() {
	for {
		if tryConnectCnt > 5 {
			klog.Errorf("%s: pinger failed to reconnect db socket finally", e.Client.System.ID)
			break
		}

		if err := e.StartConnection(); err != nil {
			tryConnectCnt++
			klog.Errorf("%s: pinger failed to reconnect db socket %v times", e.Client.System.ID, tryConnectCnt)
		} else {
			if err := e.Client.GetSystemID(); err != nil {
				klog.Errorf("%s failed to get system id: %s", appName, err)
			} else {
				klog.Infof("%s: pinger reconnect db successfully", e.Client.System.ID)
			}
			break
		}

		time.Sleep(5 * time.Second)
	}
}

// ovsMetricsUpdate updates the ovs metrics
func (e *Exporter) ovsMetricsUpdate() {
	e.exportOvsStatusGauge()
	e.exportOvsInfoGauge()
	e.exportOvsLogFileSizeGauge()
	e.exportOvsDbFileSizeGauge()
	e.exportOvsRequestErrorGauge()

	e.exportOvsDpGauge()
	e.exportOvsInterfaceGauge()
}

func (e *Exporter) exportOvsStatusGauge() {
	metricOvsHealthyStatus.Reset()
	result := e.getOvsStatus()
	for k, v := range result {
		if v {
			metricOvsHealthyStatus.WithLabelValues(e.Client.System.Hostname, k).Set(1)
		} else {
			metricOvsHealthyStatus.WithLabelValues(e.Client.System.Hostname, k).Set(0)
		}
	}
}

func (e *Exporter) exportOvsInfoGauge() {
	metricOvsInfo.Reset()
	if err := e.Client.GetSystemInfo(); err != nil {
		klog.Errorf("Failed to get System Info")
		return
	}
	metricOvsInfo.WithLabelValues(e.Client.System.ID, e.Client.System.RunDir, e.Client.System.Hostname,
		e.Client.System.Type, e.Client.System.Version, e.Client.Database.Vswitch.Version,
		e.Client.Database.Vswitch.Schema.Version).Set(1)
}

func (e *Exporter) exportOvsLogFileSizeGauge() {
	metricLogFileSize.Reset()
	components := []string{
		"ovsdb-server",
		"ovs-vswitchd",
	}
	for _, component := range components {
		file, err := e.Client.GetLogFileInfo(component)
		if err != nil {
			klog.Errorf("%s: log-file-%v", component, err)
			e.IncrementErrorCounter()
			continue
		}
		metricLogFileSize.WithLabelValues(e.Client.System.Hostname, file.Component, file.Path).Set(float64(file.Info.Size()))
	}
}

func (e *Exporter) exportOvsDbFileSizeGauge() {
	metricDbFileSize.Reset()
	database := "OVS_DB"
	fileInfo, err := os.Stat(e.Client.Database.Vswitch.File.Data.Path)
	if err != nil {
		klog.Errorf("Failed to get the DB size for database %s: %v", database, err)
		return
	}
	metricDbFileSize.WithLabelValues(e.Client.System.Hostname, database).Set(float64(fileInfo.Size()))
}

func (e *Exporter) exportOvsRequestErrorGauge() {
	metricRequestErrorNums.WithLabelValues(e.Client.System.Hostname).Set(float64(e.errors))
}

func (e *Exporter) exportOvsDpGauge() {
	datapaths, err := e.getOvsDatapath()
	if err != nil {
		klog.Errorf("Failed to get the output of ovs-dpctl dump-dps: %v", err)
		return
	}

	resetOvsDatapathMetrics()
	for _, datapathName := range datapaths {
		err = e.setOvsDpIfMetric(datapathName)
		if err != nil {
			klog.Errorf("failed to get datapath stats for %s %v", datapathName, err)
		}
	}
}

func (e *Exporter) exportOvsInterfaceGauge() {
	intfs, err := e.getInterfaceInfo()
	if err != nil {
		klog.Errorf("Failed to get the output of ovs-vsctl list Interface: %v", err)
		return
	}

	resetOvsInterfaceMetrics()
	for _, intf := range intfs {
		e.setOvsInterfaceMetric(intf)
	}
}
