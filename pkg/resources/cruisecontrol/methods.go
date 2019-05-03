package cruisecontrol

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cruise-control-methods")

func generateUrlForCC(action string, options map[string]string) string {
	optionURL := ""
	for option, value := range options {
		optionURL = optionURL + option + "=" + value + "&"
	}

	//return "http://" + serviceName + ".kafka.svc.cluster.local:8090/" + basePath + "/" + action + "?" + strings.TrimSuffix(optionURL, "&")
	//TODO only for testing
	return "http://localhost:8090/" + basePath + "/" + action + "?" + strings.TrimSuffix(optionURL, "&")
}

func postCruiseControl(action string, options map[string]string) (*http.Response, error) {

	requestURl := generateUrlForCC(action, options)
	rsp, err := http.Post(requestURl, "text/plain", nil)
	if err != nil {
		log.Error(err, "error during talking to cruise-control")
		return nil, err
	}
	if rsp.StatusCode != 200 {
		log.Error(errors.New("Non 200 response from cruise-control: "+rsp.Status), "error during talking to cruise-control")
		return nil, errors.New("Non 200 response from cruise-control: " + rsp.Status)
	}

	return rsp, nil
}

func getCruiseControl(action string, options map[string]string) (*http.Response, error) {

	requestURl := generateUrlForCC(action, options)
	rsp, err := http.Get(requestURl)
	if err != nil {
		log.Error(err, "error during talking to cruise-control")
		return nil, err
	}
	if rsp.StatusCode != 200 {
		log.Error(errors.New("Non 200 response from cruise-control: "+rsp.Status), "error during talking to cruise-control")
		return nil, errors.New("Non 200 response from cruise-control: " + rsp.Status)
	}

	return rsp, nil
}

func GetCruiseControlStatus() error {

	options := map[string]string{
		"substates": "ANALYZER",
		"json":     "true",
	}

	rsp, err := getCruiseControl(cruiseControlStateAction, options)
	if err != nil {
		log.Error(err, "can't work with cruise-control because it is not ready")
		return err
	}
	defer rsp.Body.Close()
	body, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return  err
	}

	var response map[string]interface{}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return err
	}
	if !response["AnalyzerState"].(map[string]interface{})["isProposalReady"].(bool) {
		log.Info("could not handle graceful operation because cruise-control is not ready")
		return cruiseControlNotReadyErr
	}

	return nil
}

func UpScaleCluster(brokerId string) error {

	err := GetCruiseControlStatus()
	if err != nil {
		return err
	}

	options := map[string]string{
		"json":  "true",
		"dryrun": "false",
		"brokerid": brokerId,
	}

	uResp, err := postCruiseControl(addBrokerAction, options)
	if err != nil {
		log.Error(err, "can't upscale cluster gracefully since post to cruise-control failed")
		return err
	}
	log.Info("Initiated upscale in cruise control")

	uTaskId := uResp.Header.Get("User-Task-Id")

	err = checkIfCCTaskFinished(uTaskId)
	if err != nil {
		return err
	}
	return nil
}

func DownsizeCluster(brokerId string) error {

	err := GetCruiseControlStatus()
	if err != nil {
		return err
	}

	options := map[string]string{
		"brokerid": brokerId,
		"dryrun":   "false",
		"json":     "true",
	}

	dResp, err := postCruiseControl(removeBrokerAction, options)
	if err != nil {
		log.Error(err, "can't downsize cluster gracefully since post to cruise-control failed")
		return err
	}
	log.Info("Initiated downsize in cruise control")

	uTaskId := dResp.Header.Get("User-Task-Id")

	err = checkIfCCTaskFinished(uTaskId)
	if err != nil {
		return err
	}

	return nil
}

func checkIfCCTaskFinished(uTaskId string) error {
	ccRunning := true

	for ccRunning {

		gResp, err := getCruiseControl(getTaskListAction, map[string]string{
			"json": "true",
			"user_task_ids": uTaskId,
		})
		if err != nil {
			log.Error(err, "can't downsize cluster gracefully since get to cruise-control failed")
			return err
		}

		var taskLists map[string]interface{}

		body, err := ioutil.ReadAll(gResp.Body)
		if err != nil {
			return err
		}

		err = gResp.Body.Close()
		if err != nil {
			return err
		}

		err = json.Unmarshal(body, &taskLists)
		if err != nil {
			return err
		}
		// TODO use struct insted of casting things
		for _, task := range taskLists["userTasks"].([]interface{}) {
			if task.(map[string]interface{})["Status"].(string) != "Completed" {
				ccRunning = true
				log.Info("Cruise controlel task  stillrunning", "taskID", uTaskId)
				time.Sleep(20 * time.Second)
			}
		}
		ccRunning = false
	}
	return  nil
}