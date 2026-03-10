import json
import os
from pathlib import Path

from invoke.exceptions import Exit

from tasks.e2e_framework.config import Config
from tasks.e2e_framework.tool import ask, warn


def setup_gcp_config(config: Config):
    if config.configParams is None:
        config.configParams = Config.Params(aws=None, agent=None, pulumi=None, azure=None, gcp=None)
    if config.configParams.gcp is None:
        config.configParams.gcp = Config.Params.GCP(publicKeyPath=None)

    # gcp - hardcoded to defaults temporarily
    if config.configParams.gcp.publicKeyPath is None:
        config.configParams.gcp.publicKeyPath = str(Path.home().joinpath(".ssh", "id_ed25519.pub").absolute())
    if config.configParams.gcp.pullSecretPath is None:
        config.configParams.gcp.pullSecretPath = ""


# Check if gke-gcloud-auth-plugin is installed and install it if not
def install_gcloud_auth_plugin(ctx):
    res = ctx.run('gcloud components list --format=json --filter "name: gke-gcloud-auth-plugin"', hide=True)
    installed_component = json.loads(res.stdout)
    if installed_component[0]["state"]["name"] == "Installed":
        print("✅ gke-gcloud-auth-plugin is already installed")
        return
    print("🤖 Installing gke-gcloud-auth-plugin")
    install = ctx.run("gcloud components install -q gke-gcloud-auth-plugin", hide=True)
    if install is None:
        raise Exit("Failed to install gke-gcloud-auth-plugin")
    if install.exited != 0:
        raise Exit(f"Failed to install gke-gcloud-auth-plugin: {install.stderr}")
    print("✅ gke-gcloud-auth-plugin installed")
