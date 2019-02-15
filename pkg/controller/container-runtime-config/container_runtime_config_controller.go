package containerruntimeconfig

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	ignv2_2types "github.com/coreos/ignition/config/v2_2/types"
	"github.com/golang/glog"
	"github.com/vincent-petithory/dataurl"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	coreclientsetv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"

	cligoinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	cligolistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	ctrlcommon "github.com/openshift/machine-config-operator/pkg/controller/common"
	mtmpl "github.com/openshift/machine-config-operator/pkg/controller/template"
	mcfgclientset "github.com/openshift/machine-config-operator/pkg/generated/clientset/versioned"
	"github.com/openshift/machine-config-operator/pkg/generated/clientset/versioned/scheme"
	mcfginformersv1 "github.com/openshift/machine-config-operator/pkg/generated/informers/externalversions/machineconfiguration.openshift.io/v1"
	mcfglistersv1 "github.com/openshift/machine-config-operator/pkg/generated/listers/machineconfiguration.openshift.io/v1"
	"github.com/openshift/machine-config-operator/pkg/version"
)

const (
	// maxRetries is the number of times a containerruntimeconfig pool will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the times
	// a machineconfig pool is going to be requeued:
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

var updateBackoff = wait.Backoff{
	Steps:    5,
	Duration: 100 * time.Millisecond,
	Jitter:   1.0,
}

// Controller defines the container runtime config controller.
type Controller struct {
	templatesDir string

	client        mcfgclientset.Interface
	eventRecorder record.EventRecorder

	syncHandler                   func(mcp string) error
	enqueueContainerRuntimeConfig func(*mcfgv1.ContainerRuntimeConfig)

	ccLister       mcfglistersv1.ControllerConfigLister
	ccListerSynced cache.InformerSynced

	mccrLister       mcfglistersv1.ContainerRuntimeConfigLister
	mccrListerSynced cache.InformerSynced

	imgLister       cligolistersv1.ImageLister
	imgListerSynced cache.InformerSynced

	mcpLister       mcfglistersv1.MachineConfigPoolLister
	mcpListerSynced cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

// New returns a new container runtime config controller
func New(
	templatesDir string,
	mcpInformer mcfginformersv1.MachineConfigPoolInformer,
	ccInformer mcfginformersv1.ControllerConfigInformer,
	mcrInformer mcfginformersv1.ContainerRuntimeConfigInformer,
	imgInformer cligoinformersv1.ImageInformer,
	kubeClient clientset.Interface,
	mcfgClient mcfgclientset.Interface,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&coreclientsetv1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	ctrl := &Controller{
		templatesDir:  templatesDir,
		client:        mcfgClient,
		eventRecorder: eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "machineconfigcontroller-containerruntimeconfigcontroller"}),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineconfigcontroller-containerruntimeconfigcontroller"),
	}

	mcrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addContainerRuntimeConfig,
		UpdateFunc: ctrl.updateContainerRuntimeConfig,
		DeleteFunc: ctrl.deleteContainerRuntimeConfig,
	})

	// imgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
	// 	AddFunc:    ctrl.imageConfAdded,
	// 	UpdateFunc: ctrl.imageConfUpdated,
	// 	DeleteFunc: ctrl.imageConfDeleted,
	// })

	ctrl.syncHandler = ctrl.syncContainerRuntimeConfig
	ctrl.enqueueContainerRuntimeConfig = ctrl.enqueue

	ctrl.mcpLister = mcpInformer.Lister()
	ctrl.mcpListerSynced = mcpInformer.Informer().HasSynced

	ctrl.ccLister = ccInformer.Lister()
	ctrl.ccListerSynced = ccInformer.Informer().HasSynced

	ctrl.mccrLister = mcrInformer.Lister()
	ctrl.mccrListerSynced = mcrInformer.Informer().HasSynced

	ctrl.imgLister = imgInformer.Lister()
	ctrl.imgListerSynced = imgInformer.Informer().HasSynced

	return ctrl
}

// Run executes the container runtime config controller.
func (ctrl *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer ctrl.queue.ShutDown()

	glog.Info("Starting MachineConfigController-ContainerRuntimeConfigController")
	defer glog.Info("Shutting down MachineConfigController-ContainerRuntimeConfigController")

	if !cache.WaitForCacheSync(stopCh, ctrl.mcpListerSynced, ctrl.mccrListerSynced, ctrl.ccListerSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (ctrl *Controller) updateContainerRuntimeConfig(oldObj interface{}, newObj interface{}) {
	oldCtrCfg := oldObj.(*mcfgv1.ContainerRuntimeConfig)
	newCtrCfg := newObj.(*mcfgv1.ContainerRuntimeConfig)
	// if !reflect.DeepEqual(oldCtrCfg, newCtrCfg) {
	glog.V(2).Infof("Update ContainerRuntimeConfig %s", oldCtrCfg.Name)
	ctrl.enqueueContainerRuntimeConfig(newCtrCfg)
	// }
}

func (ctrl *Controller) addContainerRuntimeConfig(obj interface{}) {
	cfg := obj.(*mcfgv1.ContainerRuntimeConfig)
	glog.V(4).Infof("Adding ContainerRuntimeConfig %s", cfg.Name)
	ctrl.enqueueContainerRuntimeConfig(cfg)
}

func (ctrl *Controller) deleteContainerRuntimeConfig(obj interface{}) {
	cfg, ok := obj.(*mcfgv1.ContainerRuntimeConfig)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		cfg, ok = tombstone.Obj.(*mcfgv1.ContainerRuntimeConfig)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a ContainerRuntimeConfig %#v", obj))
			return
		}
	}
	ctrl.cascadeDelete(cfg)
	glog.V(4).Infof("Deleted ContainerRuntimeConfig %s and restored default config", cfg.Name)
}

func (ctrl *Controller) cascadeDelete(cfg *mcfgv1.ContainerRuntimeConfig) error {
	if len(cfg.GetFinalizers()) == 0 {
		return nil
	}
	mcName := cfg.GetFinalizers()[0]
	err := ctrl.client.Machineconfiguration().MachineConfigs().Delete(mcName, &metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := ctrl.popFinalizerFromContainerRuntimeConfig(cfg); err != nil {
		return err
	}
	return nil
}

func (ctrl *Controller) enqueue(cfg *mcfgv1.ContainerRuntimeConfig) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(cfg)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", cfg, err))
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *Controller) enqueueRateLimited(cfg *mcfgv1.ContainerRuntimeConfig) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(cfg)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", cfg, err))
		return
	}
	ctrl.queue.AddRateLimited(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (ctrl *Controller) worker() {
	for ctrl.processNextWorkItem() {
	}
}

func (ctrl *Controller) processNextWorkItem() bool {
	key, quit := ctrl.queue.Get()
	if quit {
		return false
	}
	defer ctrl.queue.Done(key)

	err := ctrl.syncHandler(key.(string))
	ctrl.handleErr(err, key)

	return true
}

func (ctrl *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		ctrl.queue.Forget(key)
		return
	}

	if ctrl.queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing containerruntimeconfig %v: %v", key, err)
		ctrl.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping containerruntimeconfig %q out of the queue: %v", key, err)
	ctrl.queue.Forget(key)
	ctrl.queue.AddAfter(key, 1*time.Minute)
}

// generateOriginalContainerRuntimeConfigs returns rendered default storage, and crio config files
func (ctrl *Controller) generateOriginalContainerRuntimeConfigs(role string) (*ignv2_2types.File, *ignv2_2types.File, *ignv2_2types.File, error) {
	// Enumerate the controller config
	cc, err := ctrl.ccLister.List(labels.Everything())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not enumerate ControllerConfig %s", err)
	}
	if len(cc) == 0 {
		return nil, nil, nil, fmt.Errorf("controllerConfigList is empty")
	}
	// Render the default templates
	tmplPath := filepath.Join(ctrl.templatesDir, role)
	rc := &mtmpl.RenderConfig{ControllerConfigSpec: &cc[0].Spec}
	generatedConfigs, err := mtmpl.GenerateMachineConfigsForRole(rc, role, tmplPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generateMachineConfigsforRole failed with error %s", err)
	}
	// Find generated storage.config, and crio.config
	var (
		config, gmcStorageConfig, gmcCRIOConfig, gmcRegistriesConfig *ignv2_2types.File
		errStorage, errCRIO, errRegistries                           error
	)
	// Find storage config
	for _, gmc := range generatedConfigs {
		config, errStorage = findStorageConfig(gmc)
		if errStorage == nil {
			gmcStorageConfig = config
			break
		}
	}
	// Find CRIO config
	for _, gmc := range generatedConfigs {
		config, errCRIO = findCRIOConfig(gmc)
		if errCRIO == nil {
			gmcCRIOConfig = config
			break
		}
	}
	// Find Registries config
	for _, gmc := range generatedConfigs {
		config, errCRIO = findRegistriesConfig(gmc)
		if errRegistries == nil {
			gmcRegistriesConfig = config
			break
		}
	}
	if errStorage != nil || errCRIO != nil || errRegistries != nil {
		return nil, nil, nil, fmt.Errorf("could not generate old container runtime configs: %v, %v", errStorage, errCRIO)
	}

	return gmcStorageConfig, gmcCRIOConfig, gmcRegistriesConfig, nil
}

func (ctrl *Controller) syncStatusOnly(cfg *mcfgv1.ContainerRuntimeConfig, err error, args ...interface{}) error {
	if cfg.GetGeneration() != cfg.Status.ObservedGeneration {
		cfg.Status.ObservedGeneration = cfg.GetGeneration()
		cfg.Status.Conditions = append(cfg.Status.Conditions, wrapErrorWithCondition(err, args...))
	}
	_, lerr := ctrl.client.MachineconfigurationV1().ContainerRuntimeConfigs().UpdateStatus(cfg)
	glog.V(2).Infoln("-----status------:", cfg.Status.Conditions)
	return lerr
}

// syncContainerRuntimeConfig will sync the ContainerRuntimeconfig with the given key.
// This function is not meant to be invoked concurrently with the same key.
func (ctrl *Controller) syncContainerRuntimeConfig(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing ContainerRuntimeconfig %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing ContainerRuntimeconfig %q (%v)", key, time.Since(startTime))
	}()

	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	// Fetch the ContainerRuntimeConfig
	cfg, err := ctrl.mccrLister.Get(name)
	if errors.IsNotFound(err) {
		glog.V(2).Infof("ContainerRuntimeConfig %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Fetch the ImageConfig
	imgcfg, err := ctrl.imgLister.Get("cluster")
	if errors.IsNotFound(err) {
		glog.V(2).Infof("ImageConfig doesn't exist or has been deleted")
	} else if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	cfg = cfg.DeepCopy()
	imgcfg = imgcfg.DeepCopy()

	// Check for Deleted ContainerRuntimeConfig and optionally delete finalizers
	if cfg.DeletionTimestamp != nil {
		if len(cfg.GetFinalizers()) > 0 {
			return ctrl.cascadeDelete(cfg)
		}
		return nil
	}

	// If we have seen this generation then skip
	if cfg.Status.ObservedGeneration >= cfg.Generation {
		return nil
	}

	// Validate the ContainerRuntimeConfig CR
	if err := validateUserContainerRuntimeConfig(cfg); err != nil {
		return ctrl.syncStatusOnly(cfg, err)
	}

	// Find all MachineConfigPools
	mcpPools, err := ctrl.getPoolsForContainerRuntimeConfig(cfg)
	if err != nil {
		return ctrl.syncStatusOnly(cfg, err)
	}

	if len(mcpPools) == 0 {
		err := fmt.Errorf("containerRuntimeConfig %v does not match any MachineConfigPools", key)
		glog.V(2).Infof("%v", err)
		return ctrl.syncStatusOnly(cfg, err)
	}

	for _, pool := range mcpPools {
		role := pool.Name
		// Get MachineConfig
		managedKey := getManagedKey(pool, cfg)
		mc, err := ctrl.client.Machineconfiguration().MachineConfigs().Get(managedKey, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return ctrl.syncStatusOnly(cfg, err, "could not find MachineConfig: %v", managedKey)
		}
		isNotFound := errors.IsNotFound(err)
		// If the managed MachineConfig exists then try the next pool. This
		// prevents an infinite recursion of recreating MachineConfigs.
		if err == nil && !isNotFound && mc != nil {
			continue
		}
		// Generate the original ContainerRuntimeConfig
		originalStorageIgn, originalCRIOIgn, originalRegistriesIgn, err := ctrl.generateOriginalContainerRuntimeConfigs(role)
		if err != nil {
			return ctrl.syncStatusOnly(cfg, err, "could not generate origin ContainerRuntime Configs: %v", err)
		}

		var storageTOML, crioTOML, RegistriesTOML []byte
		ctrcfg := cfg.Spec.ContainerRuntimeConfig
		if ctrcfg.OverlaySize != (resource.Quantity{}) {
			storageTOML, err = ctrl.mergeConfigChanges(originalStorageIgn, cfg, mc, updateStorageConfig)
			if err != nil {
				glog.V(2).Infoln("------error in merging storageeeee------:", err)
				fmt.Println("------error in merging storageeeee------:", err)
				glog.V(2).Infoln(cfg, err, "error merging user changes to storage.conf: %v", err)
			}
		}
		if ctrcfg.LogLevel != "" || ctrcfg.InfraImage != "" || ctrcfg.PidsLimit != 0 || ctrcfg.LogSizeMax != (resource.Quantity{}) {
			glog.V(2).Infoln("------changing criooooooooo------:")
			crioTOML, err = ctrl.mergeConfigChanges(originalCRIOIgn, cfg, mc, updateCRIOConfig)
			if err != nil {
				glog.V(2).Infoln(cfg, err, "error merging user changes to crio.conf: %v", err)
			}
		}
		if imgcfg.Spec.RegistrySources.InsecureRegistries != nil || imgcfg.Spec.RegistrySources.BlockedRegistries != nil {
			glog.V(2).Infoln("------changing registriessss------:")
			dataURL, err := dataurl.DecodeString(originalRegistriesIgn.Contents.Source)
			if err != nil {
				glog.V(2).Infoln(cfg, err, "could not decode original registries config: %v", err)
			}
			RegistriesTOML, err = updateRegistriesConfig(dataURL.Data, imgcfg.Spec)
			if err != nil {
				glog.V(2).Infoln(cfg, err, "could not update container runtime config with new changes: %v", err)
			}
		}
		if isNotFound {
			mc = mtmpl.MachineConfigFromIgnConfig(role, managedKey, &ignv2_2types.Config{})
		}
		mc.Spec.Config = createNewCtrRuntimeConfigIgnition(storageTOML, crioTOML, RegistriesTOML)
		fmt.Println("----grrrrr-------:", string(storageTOML), string(crioTOML), string(RegistriesTOML))
		mc.ObjectMeta.Annotations = map[string]string{
			ctrlcommon.GeneratedByControllerVersionAnnotationKey: version.Version.String(),
		}
		mc.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
			metav1.OwnerReference{
				APIVersion: mcfgv1.SchemeGroupVersion.String(),
				Kind:       "ContainerRuntimeConfig",
				Name:       cfg.Name,
				UID:        cfg.UID,
			},
		}
		// Create or Update, on conflict retry
		if err := retry.RetryOnConflict(updateBackoff, func() error {
			var err error
			if isNotFound {
				_, err = ctrl.client.Machineconfiguration().MachineConfigs().Create(mc)
			} else {
				_, err = ctrl.client.Machineconfiguration().MachineConfigs().Update(mc)
			}
			return err
		}); err != nil {
			return ctrl.syncStatusOnly(cfg, err, "could not Create/Update MachineConfig: %v", err)
		}
		// Add Finalizers to the ContainerRuntimeConfigs
		if err := ctrl.addFinalizerToContainerRuntimeConfig(cfg, mc); err != nil {
			return ctrl.syncStatusOnly(cfg, err, "could not add finalizers to ContainerRuntimeConfig: %v", err)
		}
		glog.Infof("Applied ContainerRuntimeConfig %v on MachineConfigPool %v", key, pool.Name)
	}

	return ctrl.syncStatusOnly(cfg, nil)
}

// mergeConfigChanges retrieves the original/default config data from the templates, decodes it and merges in the changes given by the Custom Resource.
// It then encodes the new data and returns it.
func (ctrl *Controller) mergeConfigChanges(origFile *ignv2_2types.File, cfg *mcfgv1.ContainerRuntimeConfig, mc *mcfgv1.MachineConfig, update updateConfig) ([]byte, error) {
	dataURL, err := dataurl.DecodeString(origFile.Contents.Source)
	if err != nil {
		return nil, ctrl.syncStatusOnly(cfg, err, "could not decode original Container Runtime config: %v", err)
	}
	cfgTOML, err := update(dataURL.Data, cfg.Spec.ContainerRuntimeConfig)
	if err != nil {
		return nil, ctrl.syncStatusOnly(cfg, err, "could not update container runtime config with new changes: %v", err)
	}
	return cfgTOML, ctrl.syncStatusOnly(cfg, nil)
}

func (ctrl *Controller) popFinalizerFromContainerRuntimeConfig(ctrCfg *mcfgv1.ContainerRuntimeConfig) error {
	curJSON, err := json.Marshal(ctrCfg)
	if err != nil {
		return err
	}

	ctrCfgTmp := ctrCfg.DeepCopy()
	ctrCfgTmp.Finalizers = append(ctrCfg.Finalizers[:0], ctrCfg.Finalizers[1:]...)

	modJSON, err := json.Marshal(ctrCfgTmp)
	if err != nil {
		return err
	}

	patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(curJSON, modJSON, curJSON)
	if err != nil {
		return err
	}

	return retry.RetryOnConflict(updateBackoff, func() error {
		_, err = ctrl.client.Machineconfiguration().ContainerRuntimeConfigs().Patch(ctrCfg.Name, types.MergePatchType, patch)
		return err
	})
}

func (ctrl *Controller) addFinalizerToContainerRuntimeConfig(ctrCfg *mcfgv1.ContainerRuntimeConfig, mc *mcfgv1.MachineConfig) error {
	curJSON, err := json.Marshal(ctrCfg)
	if err != nil {
		return err
	}

	ctrCfgTmp := ctrCfg.DeepCopy()
	ctrCfgTmp.Finalizers = append(ctrCfgTmp.Finalizers, mc.Name)

	modJSON, err := json.Marshal(ctrCfgTmp)
	if err != nil {
		return err
	}

	patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(curJSON, modJSON, curJSON)
	if err != nil {
		return err
	}

	return retry.RetryOnConflict(updateBackoff, func() error {
		_, err := ctrl.client.Machineconfiguration().ContainerRuntimeConfigs().Patch(ctrCfg.Name, types.MergePatchType, patch)
		return err
	})
}

func (ctrl *Controller) getPoolsForContainerRuntimeConfig(config *mcfgv1.ContainerRuntimeConfig) ([]*mcfgv1.MachineConfigPool, error) {
	pList, err := ctrl.mcpLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	selector, err := metav1.LabelSelectorAsSelector(config.Spec.MachineConfigPoolSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid label selector: %v", err)
	}

	var pools []*mcfgv1.MachineConfigPool
	for _, p := range pList {
		// If a pool with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() || !selector.Matches(labels.Set(p.Labels)) {
			continue
		}
		pools = append(pools, p)
	}

	if len(pools) == 0 {
		return nil, fmt.Errorf("could not find any MachineConfigPool set for ContainerRuntimeConfig %s", config.Name)
	}

	return pools, nil
}
