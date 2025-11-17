import asyncio
import logging
import os

from playwright.async_api import async_playwright
from trafilatura import extract

from docreader.models.document import Document
from docreader.parser.base_parser import BaseParser
from docreader.parser.chain_parser import PipelineParser
from docreader.parser.markdown_parser import MarkdownParser
from docreader.utils import endecode

logger = logging.getLogger(__name__)


class StdWebParser(BaseParser):
    """Web page parser"""

    def __init__(self, title: str, **kwargs):
        self.title = title
        self.proxy = os.environ.get("WEB_PROXY", "")
        super().__init__(file_name=title, **kwargs)
        logger.info(f"Initialized WebParser with title: {title}")

    async def scrape(self, url: str) -> str:
        logger.info(f"Starting web page scraping for URL: {url}")
        try:
            async with async_playwright() as p:
                kwargs = {}
                if self.proxy:
                    kwargs["proxy"] = {"server": self.proxy}
                logger.info("Launching WebKit browser")
                browser = await p.webkit.launch(**kwargs)
                page = await browser.new_page()

                logger.info(f"Navigating to URL: {url}")
                try:
                    await page.goto(url, timeout=30000)
                    logger.info("Initial page load complete")
                except Exception as e:
                    logger.error(f"Error navigating to URL: {str(e)}")
                    await browser.close()
                    return ""

                logger.info("Retrieving page HTML content")
                content = await page.content()
                logger.info(f"Retrieved {len(content)} bytes of HTML content")

                await browser.close()
                logger.info("Browser closed")

            # Parse HTML content with BeautifulSoup
            logger.info("Parsing HTML with BeautifulSoup")
            logger.info("Successfully parsed HTML content")
            return content

        except Exception as e:
            logger.error(f"Failed to scrape web page: {str(e)}")
            # Return empty BeautifulSoup object on error
            return ""

    def parse_into_text(self, content: bytes) -> Document:
        """Parse web page

        Args:
            content: Web page content

        Returns:
            Parse result
        """
        url = endecode.decode_bytes(content)

        logger.info(f"Scraping web page: {url}")
        chtml = asyncio.run(self.scrape(url))
        md_text = extract(
            chtml,
            output_format="markdown",
            with_metadata=True,
            include_images=True,
            include_tables=True,
            include_links=True,
            deduplicate=True,
        )
        if not md_text:
            logger.error("Failed to parse web page")
            return Document(content=f"Error parsing web page: {url}")
        return Document(content=md_text)


class WebParser(PipelineParser):
    _parser_cls = (StdWebParser, MarkdownParser)


if __name__ == "__main__":
    logging.basicConfig(level=logging.DEBUG)
    logger.setLevel(logging.DEBUG)

    url = "https://cloud.tencent.com/document/product/457/6759"

    parser = WebParser(title="")
    cc = parser.parse_into_text(url.encode())
    with open("./tencent.md", "w") as f:
        f.write(cc.content)
