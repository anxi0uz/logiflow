from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from document_service.core.config import get_settings

engine = create_async_engine(
    get_settings().database_url,
    pool_pre_ping=True,
    hide_parameters=True,
)
session_factory = async_sessionmaker(
    bind=engine,
    autoflush=False,
    expire_on_commit=False,
)
