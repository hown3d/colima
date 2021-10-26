package podman

import (
	"encoding/json"
	"fmt"
	"os/user"
	"regexp"
	"strconv"
	"strings"
)

// setupInVM downloads the latest podman version in the VM via apt.
// since the main repo contains old versions of podman, we add the kubic project repo
func (p podmanRuntime) setupInVM() error {
	// TODO: Not working yet!
	// source /etc/os-release for version_id
	// cmd := "cat /etc/os-release | grep VERSION_ID"
	// versionID, err := p.guest.RunOutput("bash", "-c", cmd)
	// if err != nil {
	// 	return fmt.Errorf("Can't source /etc/release: %v", err)
	// }
	// versionIDSlice := strings.Split(versionID, "=")
	// err = os.Setenv(versionIDSlice[0], versionIDSlice[1])
	// if err != nil {
	// 	return fmt.Errorf("Can't set Environment variable %v: %v", versionIDSlice[0], err)
	// }

	// add kubic repo into sources.list.d
	cmd := "echo deb https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_21.04/ / | sudo tee /etc/apt/sources.list.d/devel:kubic:libcontainers:stable.list"
	err := p.guest.Run("bash", "-c", cmd)
	if err != nil {
		return fmt.Errorf("Can't add kubic repo to sources.list.d dir: %v", err)
	}

	// download kubic key and install in apt keyring
	cmd = "curl -L 'https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_21.04/Release.key' | sudo apt-key add -"
	err = p.guest.Run("bash", "-c", cmd)
	if err != nil {
		return fmt.Errorf("Can't install kubic apt key: %v", err)
	}
	// update and install podman
	err = p.guest.Run("sudo", "apt", "update")
	if err != nil {
		return fmt.Errorf("error updating apt in VM: %v", err)
	}

	// Interactive, because fuse.conf wants to be overwritten
	// TODO: Find a way to run it noninteractive
	err = p.guest.RunInteractive("sudo", "apt", "-y", "install", "podman")
	if err != nil {
		return fmt.Errorf("error installing podman in VM: %v", err)
	}

	return nil
}

func (p podmanRuntime) isInstalled() bool {
	err := p.guest.RunQuiet("command", "-v", "podman")
	return err == nil
}

// podman system connection add --identity ~/.ssh/dev_rsa testing ssh://root@server.fubar.com:2222
// createPodmanConnectionOnHost adds the remote connection to the host podman environment and sets it to default
func (p podmanRuntime) createPodmanConnectionOnHost(user *user.User, port int, vmName, rootfullSocketPath, rootlessSocketPath string) error {
	sshURI := fmt.Sprintf("ssh://%v@localhost:%v", user.Username, port)
	// rootless setup (default in podman)
	err := p.host.Run("podman", "system", "connection", "add", "--socket-path", rootlessSocketPath, "-d", vmName, "--identity", fmt.Sprintf("%v/.lima/_config/user", user.HomeDir), sshURI)
	if err != nil {
		return err
	}
	//rootfull
	return p.host.Run("podman", "system", "connection", "add", "--socket-path", rootfullSocketPath, vmName+"-root", "--identity", fmt.Sprintf("%v/.lima/_config/user", user.HomeDir), sshURI)
}

type podmanConnections struct {
	Name     string
	Identity string
	URI      string
}

type limaVM struct {
	Name         string
	Status       string
	Dir          string
	Arch         string
	SSHLocalPort int
	HostAgentPID int
	QemuPID      int
}

func (p podmanRuntime) getSSHPortFromLimactl() (int, error) {
	// get ssh port from limactl since sshport environment seems to be always different
	limaVMJSON, err := p.host.RunOutput("limactl", "list", "--json")
	if err != nil {
		return 0, fmt.Errorf("Can't get lima VMs on host: %v", err)
	}
	for _, vmJSON := range strings.Split(limaVMJSON, "\n") {
		var vm limaVM
		err = json.Unmarshal([]byte(vmJSON), &vm)
		if err != nil {
			return 0, fmt.Errorf("Can't unmarshal lima VMs json %v", err)
		}
		if vm.Name == "colima" {
			return vm.SSHLocalPort, nil
		}
	}
	return 0, fmt.Errorf("Colima VM wasn't found in lima vms")
}

func (p podmanRuntime) checkIfPodmanRemoteConnectionIsValid(sshPort int, vmName string) (bool, error) {
	connectionJSON, err := p.host.RunOutput("podman", "system", "connection", "list", "--format", "json")
	if err != nil {
		return false, fmt.Errorf("Can't get podman connections on host: %v", err)
	}
	var connections []podmanConnections
	err = json.Unmarshal([]byte(connectionJSON), &connections)
	if err != nil {
		return false, fmt.Errorf("Can't unmarshal podman connections json: %v", err)
	}
	re := regexp.MustCompile(fmt.Sprintf(`%v.*`, vmName))
	for _, connection := range connections {
		if re.MatchString(connection.Name) {
			//ssh://foo@bar:SSHPORT
			return strings.Split(connection.URI, ":")[2] == strconv.Itoa(sshPort), nil
		}
	}
	return false, nil
}

func (p podmanRuntime) checkIfPodmanIsRunning() (bool, error) {
	cmd := "ps -ef | grep 'podman system service' | wc -l"
	output, err := p.guest.RunOutput("bash", "-c", cmd)
	if err != nil {
		return false, fmt.Errorf("Can't check if podman Socket is running in VM: %v", err)
	}
	wordCount, err := strconv.Atoi(output)
	if err != nil {
		return false, err
	}
	// bash command itself and grep command should be excluded
	return wordCount != 2, nil
}