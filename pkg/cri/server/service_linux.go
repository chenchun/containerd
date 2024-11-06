/*
   Copyright The containerd Authors.

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

package server

import (
	"encoding/json"
	"fmt"

	cni "github.com/containerd/go-cni"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/pkg/cap"
	"github.com/containerd/containerd/pkg/userns"
)

// networkAttachCount is the minimum number of networks the PodSandbox
// attaches to
const networkAttachCount = 2

// initPlatform handles linux specific initialization for the CRI service.
func (c *criService) initPlatform() (err error) {
	if userns.RunningInUserNS() {
		if !(c.config.DisableCgroup && !c.apparmorEnabled() && c.config.RestrictOOMScoreAdj) {
			logrus.Warn("Running containerd in a user namespace typically requires disable_cgroup, disable_apparmor, restrict_oom_score_adj set to be true")
		}
	}

	if c.config.EnableSelinux {
		if !selinux.GetEnabled() {
			logrus.Warn("Selinux is not supported")
		}
		if r := c.config.SelinuxCategoryRange; r > 0 {
			selinux.CategoryRange = uint32(r)
		}
	} else {
		selinux.SetDisabled()
	}

	pluginDirs := map[string]string{
		defaultNetworkPlugin: c.config.NetworkPluginConfDir,
	}
	for name, conf := range c.config.Runtimes {
		if conf.NetworkPluginConfDir != "" {
			pluginDirs[name] = conf.NetworkPluginConfDir
		}
	}
	logrus.Infof("rami pluginDirs :%v", pluginDirs)
	c.netPlugin = make(map[string]cni.CNI)
	for name, dir := range pluginDirs {
		max := c.config.NetworkPluginMaxConfNum
		if name != defaultNetworkPlugin {
			if m := c.config.Runtimes[name].NetworkPluginMaxConfNum; m != 0 {
				max = m
			}
		}
		// Pod needs to attach to at least loopback network and a non host network,
		// hence networkAttachCount is 2. If there are more network configs the
		// pod will be attached to all the networks but we will only use the ip
		// of the default network interface as the pod IP.
		i, err := cni.New(cni.WithMinNetworkCount(networkAttachCount),
			cni.WithPluginConfDir(dir),
			cni.WithPluginMaxConfNum(max),
			cni.WithPluginDir([]string{c.config.NetworkPluginBinDir}))
		if err != nil {
			return fmt.Errorf("failed to initialize cni: %w", err)
		}
		c.netPlugin[name] = i
		confData, err := json.Marshal(i.GetConfig())
		if err != nil {
			return fmt.Errorf("failed to marshal plugin config: %w", err)
		}
		logrus.Infof("rami plugin config for %s: %s", name, string(confData))
	}

	if c.allCaps == nil {
		c.allCaps, err = cap.Current()
		if err != nil {
			return fmt.Errorf("failed to get caps: %w", err)
		}
	}

	return nil
}

// cniLoadOptions returns cni load options for the linux.
func (c *criService) cniLoadOptions() []cni.Opt {
	return []cni.Opt{cni.WithLoNetwork, cni.WithDefaultConf}
}
