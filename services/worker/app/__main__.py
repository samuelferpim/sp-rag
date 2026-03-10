"""
Entrypoint — invoked via: python -m app

Sets up structured logging and starts the Kafka consumer loop.
"""

import logging
import sys

from dotenv import load_dotenv

from app.config import load_config
from app.consumer import run


def _configure_logging() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
        stream=sys.stdout,
    )


def main() -> None:
    load_dotenv()  # no-op if .env is absent (e.g. in production with real env)
    _configure_logging()

    logger = logging.getLogger(__name__)
    logger.info("SP-RAG worker starting")

    try:
        config = load_config()
    except ValueError as exc:
        logger.critical("Configuration error: %s", exc)
        sys.exit(1)

    run(config)


if __name__ == "__main__":
    main()
