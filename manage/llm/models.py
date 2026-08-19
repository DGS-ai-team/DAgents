from __future__ import annotations
from pydantic import BaseModel, Field

_PROVIDERS = {"openai", "deepseek", "qwen", "vllm", "glm", "minimax", "mimo"}

class LLMConfigCreate(BaseModel):
    name: str = Field(min_length=1)
    provider: str
    base_url: str = Field(min_length=1)
    model: str = Field(min_length=1)
    api_key: str = ""
    clear_api_key: bool = False
    reasoning_effort: str | None = None
    thinking: str | None = None
    is_default: bool = False
    allowed_groups: list[str] = Field(default_factory=list)

    def normalized_provider(self) -> str:
        p = self.provider.strip().lower()
        return p if p in _PROVIDERS else "openai"

class LLMConfig(LLMConfigCreate):
    id: str
    created_at: int
    updated_at: int

class LLMConfigMasked(LLMConfig):
    has_api_key: bool = False

class LLMResolved(BaseModel):
    model: str
    baseURL: str
    apiKey: str

class LLMProbeRequest(BaseModel):
    base_url: str = Field(min_length=1)
    api_key: str = ""
    provider: str = ""
    config_id: str | None = None

class LLMProbeResponse(BaseModel):
    models: list[dict[str, str]]
    suggested_provider: str = ""
