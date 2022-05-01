/*
Copyright 2022 The KCP Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	cli "github.com/virtual-kubelet/node-cli"
	logruscli "github.com/virtual-kubelet/node-cli/logrus"
	"github.com/virtual-kubelet/node-cli/opts"
	"github.com/virtual-kubelet/node-cli/provider"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	logruslogger "github.com/virtual-kubelet/virtual-kubelet/log/logrus"
	"golang.org/x/time/rate"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/k8s-cloud-platform/vkubelet/pkg/controllers"
	vprovider "github.com/k8s-cloud-platform/vkubelet/pkg/provider"
	"github.com/k8s-cloud-platform/vkubelet/pkg/util"
)

var (
	buildVersion         = "N/A"
	buildTime            = "N/A"
	k8sVersion           = "v1.23.4"
	numberOfWorkers      = 50
	ignoreLabels         = ""
	enableControllers    = ""
	enableServiceAccount = true
	providerName         = "k8s"
)

func main() {
	var cc vprovider.ClientConfig
	ctx := cli.ContextWithCancelOnSignal(context.Background())
	flags := pflag.NewFlagSet("client", pflag.ContinueOnError)
	flags.IntVar(&cc.KubeClientBurst, "client-burst", 1000, "qpi burst for client cluster.")
	flags.IntVar(&cc.KubeClientQPS, "client-qps", 500, "qpi qps for client cluster.")
	flags.StringVar(&cc.ClientKubeConfigPath, "client-kubeconfig", "", "kubeconfig for client cluster.")
	flags.StringVar(&ignoreLabels, "ignore-labels", util.BatchPodLabel,
		fmt.Sprintf("ignore-labels are the labels we would like to ignore when build pod for client clusters, "+
			"usually these labels will infulence schedule, default %v, multi labels should be seperated by comma(,"+
			")", util.BatchPodLabel))
	flags.StringVar(&enableControllers, "enable-controllers", "PVControllers,ServiceControllers",
		"support PVControllers,ServiceControllers, default, all of these")

	flags.BoolVar(&enableServiceAccount, "enable-serviceaccount", true,
		"enable service account for pods, default true")

	logger := logrus.StandardLogger()

	log.L = logruslogger.FromLogrus(logrus.NewEntry(logger))
	logConfig := &logruscli.Config{LogLevel: "info"}

	o, err := opts.FromEnv()
	if err != nil {
		panic(err)
	}
	o.Provider = providerName
	o.PodSyncWorkers = numberOfWorkers
	o.Version = strings.Join([]string{k8sVersion, providerName, buildVersion}, "-")
	o.SyncPodsFromKubernetesRateLimiter = rateLimiter()
	o.DeletePodsFromKubernetesRateLimiter = rateLimiter()
	o.SyncPodStatusFromProviderRateLimiter = rateLimiter()
	node, err := cli.New(ctx,
		cli.WithBaseOpts(o),
		cli.WithProvider(providerName, func(cfg provider.InitConfig) (provider.Provider, error) {
			cfg.ConfigPath = o.KubeConfigPath
			provider, err := vprovider.NewVirtualK8S(cfg, &cc, ignoreLabels, enableServiceAccount, o)
			if err == nil {
				go RunController(ctx, provider, cfg.NodeName, numberOfWorkers)
			}
			return provider, err
		}),
		cli.WithCLIVersion(buildVersion, buildTime),
		cli.WithKubernetesNodeVersion(k8sVersion),
		cli.WithPersistentFlags(logConfig.FlagSet()),
		cli.WithPersistentFlags(flags),
		cli.WithPersistentPreRunCallback(func() error {
			return logruscli.Configure(logConfig, logger)
		}),
	)

	if err != nil {
		log.G(ctx).Fatal(err)
	}

	if err := node.Run(ctx); err != nil {
		log.G(ctx).Fatal(err)
	}
}

// RunController starts controllers for objects needed to be synced
func RunController(ctx context.Context, p *vprovider.VirtualK8S, hostIP string,
	workers int) *controllers.ServiceController {
	master := p.GetMaster()
	client := p.GetClient()
	masterInformer := kubeinformers.NewSharedInformerFactory(master, 0)
	if masterInformer == nil {
		return nil
	}
	clientInformer := kubeinformers.NewSharedInformerFactory(client, 1*time.Minute)
	if clientInformer == nil {
		return nil
	}

	runningControllers := []controllers.Controller{buildCommonControllers(client, masterInformer, clientInformer)}

	controllerSlice := strings.Split(enableControllers, ",")
	for _, c := range controllerSlice {
		if len(c) == 0 {
			continue
		}
		switch c {
		case "PVControllers":
			pvCtrl := controllers.NewPVController(master, client, masterInformer, clientInformer, hostIP)
			runningControllers = append(runningControllers, pvCtrl)
		case "ServiceControllers":
			serviceCtrl := controllers.NewServiceController(master, client, masterInformer, clientInformer, p.GetNameSpaceLister())
			runningControllers = append(runningControllers, serviceCtrl)
		default:
			klog.Warningf("Skip: %v", c)
		}
	}
	masterInformer.Start(ctx.Done())
	clientInformer.Start(ctx.Done())
	for _, ctrl := range runningControllers {
		go ctrl.Run(workers, ctx.Done())
	}
	<-ctx.Done()
	return nil
}

func buildCommonControllers(client kubernetes.Interface, masterInformer,
	clientInformer kubeinformers.SharedInformerFactory) controllers.Controller {

	configMapRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	secretRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)

	return controllers.NewCommonController(client, masterInformer, clientInformer, configMapRateLimiter, secretRateLimiter)
}

func rateLimiter() workqueue.RateLimiter {
	return workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 10*time.Second),
		// 100 qps, 1000 bucket size.  This is only for retry speed and its only the overall factor (not per item)
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(100), 1000)},
	)
}
