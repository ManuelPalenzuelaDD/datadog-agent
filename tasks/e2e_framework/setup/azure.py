import os
from pathlib import Path

from tasks.e2e_framework.config import Config
from tasks.e2e_framework.tool import ask, warn


def setup_azure_config(config: Config):
    if config.configParams is None:
        config.configParams = Config.Params(aws=None, agent=None, pulumi=None, azure=None, gcp=None)
    if config.configParams.azure is None:
        config.configParams.azure = Config.Params.Azure(publicKeyPath=None)

    # azure public key path - hardcoded to default temporarily
    if config.configParams.azure.publicKeyPath is None:
        config.configParams.azure.publicKeyPath = str(Path.home().joinpath(".ssh", "id_ed25519.pub").absolute())
