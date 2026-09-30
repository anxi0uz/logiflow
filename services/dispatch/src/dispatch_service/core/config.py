from functools import cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    debug: bool = False
    database_url: str | None = None
    database_host: str = "127.0.0.1"
    database_port: int = 5432
    database_name: str = "logiflow"
    database_user: str = "dispatch_reader"
    database_password: str | None = None
    nats_url: str = "nats://127.0.0.1:4222"
    grpc_host: str = "0.0.0.0"
    grpc_port: int = 50052

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )


@cache
def get_settings() -> Settings:
    return Settings()  # pyright: ignore[reportCallIssue]
