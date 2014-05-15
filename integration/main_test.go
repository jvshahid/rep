package integration_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cloudfoundry-incubator/rep/reprunner"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/onsi/gomega/ghttp"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

var representativePath string
var etcdRunner *etcdstorerunner.ETCDClusterRunner
var runner *reprunner.Runner

var _ = Describe("Main", func() {
	var (
		fakeExecutor *ghttp.Server
		bbs          *Bbs.BBS
	)

	BeforeEach(func() {
		fakeExecutor = ghttp.NewServer()
		etcdPort := 5001 + GinkgoParallelNode()
		schedulerPort := 56000 + GinkgoParallelNode()

		etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
		etcdRunner.Start()

		bbs = Bbs.NewBBS(etcdRunner.Adapter(), timeprovider.NewTimeProvider())

		runner = reprunner.New(
			representativePath,
			"the-stack",
			fmt.Sprintf("127.0.0.1:%d", schedulerPort),
			fakeExecutor.URL(),
			fmt.Sprintf("http://127.0.0.1:%d", etcdPort),
			"info",
		)

		runner.Start()
	})

	AfterEach(func() {
		runner.KillWithFire()
		fakeExecutor.Close()
		etcdRunner.Stop()
	})

	Describe("when a kill signal is send to the representative", func() {
		BeforeEach(func() {
			runner.Stop()
		})

		It("should die", func() {
			Eventually(runner.Session.ExitCode).Should(Equal(0))
		})
	})

	Describe("when a task is written to the BBS", func() {
		BeforeEach(func() {
			fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("POST", "/containers"),
				ghttp.RespondWith(http.StatusCreated, `{"executor_guid":"executor-guid","guid":"guid-123"}`)),
			)

			bbs.DesireTask(models.Task{
				Stack: "the-stack",
			})
		})

		It("makes a request to the executor", func() {
			Eventually(fakeExecutor.ReceivedRequests).Should(HaveLen(1))
		})
	})

	Describe("when an LRP is written to the BBS", func() {
		BeforeEach(func() {
			fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("POST", "/containers"),
				ghttp.RespondWith(http.StatusCreated, `{"executor_guid":"executor-guid","guid":"guid-123"}`)),
			)

			bbs.DesireTransitionalLongRunningProcess(models.TransitionalLongRunningProcess{
				Stack: "the-stack",
			})
		})

		It("makes a request to the executor", func() {
			Eventually(fakeExecutor.ReceivedRequests).Should(HaveLen(1))
		})
	})
})

func TestRepresentativeMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var _ = BeforeSuite(func() {
	var err error
	representativePath, err = gexec.Build("github.com/cloudfoundry-incubator/rep", "-race")
	Ω(err).ShouldNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
	if etcdRunner != nil {
		etcdRunner.Stop()
	}
	if runner != nil {
		runner.KillWithFire()
	}
})
