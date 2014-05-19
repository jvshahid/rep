package taskcomplete

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/client"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	bbs    bbs.RepBBS
	logger *gosteno.Logger
}

func NewHandler(bbs bbs.RepBBS, logger *gosteno.Logger) http.Handler {
	return &handler{
		bbs:    bbs,
		logger: logger,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestBody, err := ioutil.ReadAll(r.Body)
	r.Body.Close()

	runResult, err := client.NewContainerRunResultFromJSON(requestBody)
	if err != nil {
		handler.logger.Errord(map[string]interface{}{
			"error": fmt.Sprintf("Could not unmarshal response: %s", err),
		}, "game-scheduler.complete-callback-handler.failed")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	task := models.Task{}
	err = json.Unmarshal(runResult.Metadata, &task)
	if err != nil {
		handler.logger.Errord(map[string]interface{}{
			"error": fmt.Sprintf("Could not unmarshal metadata: %s", err),
		}, "game-scheduler.complete-callback-handler.failed")
		return
	}

	handler.bbs.CompleteTask(task, runResult.Failed, runResult.FailureReason, runResult.Result)
}
