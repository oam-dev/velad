package pkg

import (
	"fmt"
	"github.com/oam-dev/velad/pkg/apis"
	"github.com/oam-dev/velad/pkg/handler"
	"github.com/oam-dev/velad/pkg/utils"
	"github.com/oam-dev/velad/version"
	"github.com/pkg/errors"
	"os"
	"runtime"

	"github.com/oam-dev/kubevela/pkg/utils/common"
	cmdutil "github.com/oam-dev/kubevela/pkg/utils/util"
	"github.com/oam-dev/kubevela/references/cli"
	"github.com/spf13/cobra"
)

var (
	cArgs apis.InstallArgs
	errf  = utils.Errf
	info  = utils.Info
	infof = utils.Infof
	h     = handler.DefaultHandler
)

// NewVeladCommand create velad command
func NewVeladCommand() *cobra.Command {
	ioStreams := cmdutil.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	c := common.Args{
		Schema: common.Scheme,
	}
	cmd := &cobra.Command{
		Use:   "velad",
		Short: "Setup a KubeVela control plane air-gapped",
		Long:  "Setup a KubeVela control plane air-gapped, using K3s and only for Linux now",
	}
	cmd.AddCommand(
		NewInstallCmd(c, ioStreams),
		NewLoadBalancerCmd(),
		NewKubeConfigCmd(),
		NewTokenCmd(),
		NewUninstallCmd(),
		NewVersionCmd(),
	)
	return cmd
}

func NewTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print control plane token",
		Long:  "Print control plane token, only works if control plane has been set up",
		Run: func(cmd *cobra.Command, args []string) {
			tokenLoc := "/var/lib/rancher/k3s/server/token"
			_, err := os.Stat(tokenLoc)
			if err == nil {
				file, err := os.ReadFile("/var/lib/rancher/k3s/server/token")
				if err != nil {
					errf("Fail to read token file: %s: %v\n", tokenLoc, err)
					return
				}
				fmt.Println(string(file))
				return
			}
			info("No token found, control plane not set up yet.")
		},
	}
	return cmd
}

// NewInstallCmd create install cmd
func NewInstallCmd(c common.Args, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Quickly setup a KubeVela control plane",
		Long:  "Quickly setup a KubeVela control plane, using K3s and only for Linux now",
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			defer func() {
				err := utils.Cleanup()
				if err != nil {
					errf("Fail to clean up: %v\n", err)
				}
			}()

			// Step.1 Set up K3s as control plane cluster
			err = h.Install(cArgs)
			if err != nil {
				return errors.Wrap(err, "Fail to set up cluster")
			}

			// Step.2 Deal with KUBECONFIG
			err = h.GenKubeconfig(cArgs.BindIP)
			if err != nil {
				return errors.Wrap(err, "fail to generate kubeconfig")
			}
			err = h.SetKubeconfig()
			if err != nil {
				return errors.Wrap(err, "fail to set kubeconfig")
			}

			// Step.3 Install Vela CLI
			LinkToVela()

			// Step.4 load vela-core images
			err = LoadVelaImages()
			if err != nil {
				return errors.Wrap(err, "fail to load vela images")
			}

			if !cArgs.ClusterOnly {

				// Step.5 save vela-core chart and velaUX addon
				chart, err := PrepareVelaChart()
				if err != nil {
					return errors.Wrap(err, "fail to prepare vela chart")
				}
				err = PrepareVelaUX()
				if err != nil {
					return errors.Wrap(err, "fail to prepare vela UX")
				}
				// Step.6 install vela-core
				info("Installing vela-core Helm chart...")
				ioStreams.Out = utils.VeladWriter{W: os.Stdout}
				installCmd := cli.NewInstallCommand(c, "1", ioStreams)
				installArgs := []string{"--file", chart, "--detail=false", "--version", version.VelaVersion}
				if utils.IfDeployByPod(cArgs.Controllers) {
					installArgs = append(installArgs, "--set", "deployByPod=true")
				}
				userDefinedArgs := utils.TransArgsToString(cArgs.InstallArgs)
				installArgs = append(installArgs, userDefinedArgs...)
				installCmd.SetArgs(installArgs)
				err = installCmd.Execute()
				if err != nil {
					errf("Didn't install vela-core in control plane: %v. You can try \"vela install\" later\n", err)
				}
			}

			utils.WarnSaveToken(cArgs.Token)
			info("Successfully install KubeVela control plane! Try: vela components")
			return nil
		},
	}
	cmd.Flags().BoolVar(&cArgs.ClusterOnly, "cluster-only", false, "If set, start cluster without installing vela-core, typically used when restart a control plane where vela-core has been installed")
	cmd.Flags().StringVar(&cArgs.DBEndpoint, "database-endpoint", "", "Use an external database to store control plane metadata, please ref https://rancher.com/docs/k3s/latest/en/installation/datastore/#datastore-endpoint-format-and-functionality for the format")
	cmd.Flags().StringVar(&cArgs.BindIP, "bind-ip", "", "Bind additional hostname or IP in the kubeconfig TLS cert")
	cmd.Flags().StringVar(&cArgs.Token, "token", "", "Token for identify the cluster. Can be used to restart the control plane or register other node. If not set, random token will be generated")
	cmd.Flags().StringVar(&cArgs.Controllers, "controllers", "*", "A list of controllers to enable, check \"--controllers\" argument for more spec in https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/")
	cmd.Flags().StringVar(&cArgs.Name, "name", "default", "The name of the cluster. only works when NOT in linux environment")

	// inherit args from `vela install`
	cmd.Flags().StringArrayVarP(&cArgs.InstallArgs.Values, "set", "", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	cmd.Flags().StringVarP(&cArgs.InstallArgs.Namespace, "namespace", "n", "vela-system", "namespace scope for installing KubeVela Core")
	cmd.Flags().BoolVarP(&cArgs.InstallArgs.Detail, "detail", "d", true, "show detail log of installation")
	cmd.Flags().BoolVarP(&cArgs.InstallArgs.ReuseValues, "reuse", "r", true, "will re-use the user's last supplied values.")

	return cmd
}

// NewKubeConfigCmd create kubeconfig command for ctrl-plane
func NewKubeConfigCmd() *cobra.Command {
	kArgs:=apis.KubeconfigArgs{}
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "print kubeconfig to access control plane",
		RunE: func(cmd *cobra.Command, args []string) error{
			err := kArgs.Validate()
			if err != nil {
				return errors.Wrap(err,"validate kubeconfig args")
			}
			return handler.PrintKubeConfig(kArgs)
		},
	}
	cmd.Flags().StringVarP(&kArgs.Name, "name", "n", "default", "The name of cluster, Only works in macOS/Windows")
	cmd.Flags().BoolVar(&kArgs.Internal, "internal", false, "Print kubeconfig that used in Docker network. Typically used in \"vela cluster join\". Only works in macOS/Windows. ")
	cmd.Flags().BoolVar(&kArgs.External, "external", false, "Print kubeconfig that can be used on other machine")
	cmd.Flags().BoolVar(&kArgs.Host, "host", false, "Print kubeconfig path that can be used in this machine")
	return cmd
}

// NewUninstallCmd create uninstall command
func NewUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "uninstall control plane",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := cmd.Flags().GetString("name")
			if err != nil {
				return err
			}
			err = h.Uninstall(name)
			if err != nil {
				return errors.Wrap(err, "Failed to uninstall KubeVela control plane")
			}
			info("Successfully uninstall KubeVela control plane!")
			return nil
		},
	}
	cmd.Flags().StringP("name", "n", "default", "The name of the control plane. Only works when NOT in linux environment")
	return cmd
}

func NewLoadBalancerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load-balancer",
		Short: "Configure load balancer between nodes set up by VelaD",
		Long:  "Configure load balancer between nodes set up by VelaD",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return errors.New("Load balancer is only supported on linux")
			}
			return nil
		},
	}
	cmd.AddCommand(
		NewLBInstallCmd(),
		NewLBUninstallCmd(),
	)
	return cmd
}

func NewLBInstallCmd() *cobra.Command {
	var LBArgs apis.LoadBalancerArgs
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Setup load balancer between nodes set up by VelaD",
		Long:  "Setup load balancer between nodes set up by VelaD",
		Run: func(cmd *cobra.Command, args []string) {
			defer func() {
				err := utils.Cleanup()
				if err != nil {
					errf("Fail to clean up: %v\n", err)
				}
			}()

			if len(LBArgs.Hosts) == 0 {
				errf("Must specify one host at least\n")
				os.Exit(1)
			}
			err := ConfigureNginx(LBArgs)
			if err != nil {
				errf("Fail to setup load balancer (nginx): %v\n", err)
			}
			info("Successfully setup load balancer!")
		},
	}
	cmd.Flags().StringSliceVar(&LBArgs.Hosts, "host", []string{}, "Host IPs of control plane node installed by velad, can be specified multiple or separate value by comma like: IP1,IP2")
	cmd.Flags().StringVarP(&LBArgs.Configuration, "conf", "c", "", "(Optional) Specify the nginx configuration file place, this file will be overwrite")
	return cmd
}

func NewLBUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall load balancer",
		Long:  "Uninstall load balancer installed by VelaD",
		Run: func(cmd *cobra.Command, args []string) {
			err := UninstallNginx()
			if err != nil {
				errf("Fail to uninstall load balancer (nginx): %v\n", err)
			}
			err = KillNginx()
			if err != nil {
				errf("Fail to kill nginx process: %v\n", err)
			}
		},
	}
	return cmd
}

func NewVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Prints VelaD build version information",
		Long:  "Prints VelaD build version information.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Core Version: %s\n", version.VelaVersion)
			fmt.Printf("VelaD Version: %s\n", version.VelaDVersion)
		},
	}
	return cmd

}
