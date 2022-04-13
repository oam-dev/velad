package pkg

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

// PrepareK3sImages Write embed images
func PrepareK3sImages() error {
	embedK3sImage, err := K3sDirectory.Open("static/k3s/k3s-airgap-images-amd64.tar.gz")
	if err != nil {
		return err
	}
	defer CloseQuietly(embedK3sImage)
	err = os.MkdirAll(k3sImageDir, 600)
	if err != nil {
		return err
	}
	/* #nosec */
	bin, err := os.OpenFile(k3sImageLocation, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer CloseQuietly(bin)
	_, err = io.Copy(bin, embedK3sImage)
	if err != nil {
		return err
	}
	unGzipCmd := exec.Command("gzip", "-f", "-d", k3sImageLocation)
	output, err := unGzipCmd.CombinedOutput()
	fmt.Print(string(output))
	if err != nil {
		return err
	}
	info("Successfully prepare k3s image")
	return nil
}

// PrepareK3sScript Write k3s install script to local
func PrepareK3sScript() (string, error) {
	embedScript, err := K3sDirectory.Open("static/k3s/setup.sh")
	if err != nil {
		return "", err
	}
	scriptName, err := SaveToTemp(embedScript, "k3s-setup-*.sh")
	if err != nil {
		return "", err
	}
	return scriptName, nil
}

// PrepareK3sBin prepare k3s bin
func PrepareK3sBin() error {
	embedK3sBinary, err := K3sDirectory.Open("static/k3s/k3s")
	if err != nil {
		return err
	}
	defer CloseQuietly(embedK3sBinary)
	/* #nosec */
	bin, err := os.OpenFile(k3sBinaryLocation, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer CloseQuietly(bin)
	_, err = io.Copy(bin, embedK3sBinary)
	if err != nil {
		return err
	}
	info("Successfully place k3s binary to " + k3sBinaryLocation)
	return nil
}

// SetupK3s will set up K3s as control plane.
func SetupK3s(cArgs CtrlPlaneArgs) error {
	info("Preparing cluster setup script...")
	script, err := PrepareK3sScript()
	if err != nil {
		return errors.Wrap(err, "fail to prepare k3s setup script")
	}

	info("Preparing k3s binary...")
	err = PrepareK3sBin()
	if err != nil {
		return errors.Wrap(err, "Fail to prepare k3s binary")
	}

	info("Preparing k3s images")
	err = PrepareK3sImages()
	if err != nil {
		return errors.Wrap(err, "Fail to prepare k3s images")
	}

	info("Setting up cluster...")
	args := []string{script}
	other := composeArgs(cArgs)
	args = append(args, other...)
	/* #nosec */
	cmd := exec.Command("/bin/bash", args...)

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "INSTALL_K3S_SKIP_DOWNLOAD=true")
	output, err := cmd.CombinedOutput()
	fmt.Print(string(output))
	return errors.Wrap(err, "K3s install script failed")
}

// composeArgs convert args from command to ones passed to k3s install script
func composeArgs(args CtrlPlaneArgs) []string {
	var shellArgs []string
	if args.DBEndpoint != "" {
		shellArgs = append(shellArgs, "--datastore-endpoint="+args.DBEndpoint)
	}
	if args.BindIP != "" {
		shellArgs = append(shellArgs, "--tls-san="+args.BindIP)
	}
	if args.Token != "" {
		shellArgs = append(shellArgs, "--token="+args.Token)
	}
	if args.Controllers != "*" {
		shellArgs = append(shellArgs, "--kube-controller-manager-arg=controllers="+args.Controllers)
		// TODO : deal with coredns/local-path-provisioner/metrics-server Deployment when no deployment controllers
		if !HaveController(args.Controllers, "job") {
			// Traefik use Job to install, which is impossible without Job Controller
			shellArgs = append(shellArgs, "--disable", "traefik")
		}
	}
	return shellArgs
}

// GenKubeconfig will generate kubeconfig for remote access.
// This won't modify the origin kubeconfig generated by k3s
func GenKubeconfig(bindIP string) error {
	var err error
	if bindIP != "" {
		info("Generating kubeconfig for remote access into ", ExternalKubeConfigLocation)
		originConf, err := os.ReadFile(KubeConfigLocation)
		if err != nil {
			return err
		}
		newConf := strings.Replace(string(originConf), "127.0.0.1", bindIP, 1)
		err = os.WriteFile(ExternalKubeConfigLocation, []byte(newConf), 600)
	}
	info("Successfully generate kubeconfig at ", ExternalKubeConfigLocation)
	return err
}
