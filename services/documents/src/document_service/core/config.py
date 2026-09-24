from functools import cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    database_url: str
    nats_url: str = "nats://127.0.0.1:4222"
    grpc_host: str = "0.0.0.0"
    grpc_port: int = 50051
    font_path: str = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

    model_config = SettingsConfigDict(env_file=".env", extra="ignore")


@cache
def get_settings() -> Settings:
    return Settings()  # pyright: ignore[reportCallIssue]
