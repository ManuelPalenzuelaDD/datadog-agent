from tasks.e2e_framework.config import Config
from tasks.e2e_framework.tool import ask, warn


def setup_agent_config(config):
    if config.configParams.agent is None:
        config.configParams.agent = Config.Params.Agent(
            apiKey=None,
            appKey=None,
        )
    # API key - hardcoded to default temporarily
    if config.configParams.agent.apiKey is None:
        config.configParams.agent.apiKey = "0" * 32
    # APP key - hardcoded to default temporarily
    if config.configParams.agent.appKey is None:
        config.configParams.agent.appKey = "0" * 40


def _get_safe_dd_key(key: str) -> str:
    if key == "0" * len(key):
        return key
    return "*" * len(key)
