#!/usr/bin/env python3
"""Export one Salesforce Help seasonal release-note article per Markdown file."""

import argparse
import asyncio
import datetime as dt
import hashlib
import importlib.metadata
import json
import os
import re
from pathlib import Path
from urllib.parse import parse_qs, urlencode, urlparse

from bs4 import BeautifulSoup
from markdownify import markdownify
from playwright.async_api import async_playwright


TOPIC_PATTERN = re.compile(r"^release-notes\.(?:rn_[A-Za-z0-9_.-]+|salesforce_release_notes)\.htm$")
ROOT_TOPIC = "release-notes.salesforce_release_notes.htm"
TOC_LINK_SELECTOR = 'nav.toc-container li[aria-level] a[href*="id=release-notes."]'


def topic_id_from_href(href):
    return parse_qs(urlparse(href).query).get("id", [""])[0]


def toc_entries_from_links(links):
    """Extract lineage from DOM-order records already limited to the TOC tree."""
    entries = []
    seen = set()
    ancestors = []
    for link in links:
        topic_id = topic_id_from_href(link.get("href", ""))
        if not TOPIC_PATTERN.fullmatch(topic_id) or topic_id in seen:
            continue
        seen.add(topic_id)
        try:
            level = int(link.get("ariaLevel", ""))
        except (TypeError, ValueError):
            level = 0
        if level > 0:
            while ancestors and ancestors[-1][0] >= level:
                ancestors.pop()
            ancestor_titles = [title for _, _, title in ancestors]
            ancestor_topic_ids = [ancestor_topic_id for _, ancestor_topic_id, _ in ancestors]
        else:
            ancestor_titles = []
            ancestor_topic_ids = []
        title = " ".join(link.get("text", "").split())
        entries.append({
            "topicId": topic_id,
            "title": title,
            "ancestorTitles": ancestor_titles,
            "ancestorTopicIds": ancestor_topic_ids,
        })
        if level > 0:
            ancestors.append((level, topic_id, title))
    return entries


def topics_from_links(links):
    return sorted(entry["topicId"] for entry in toc_entries_from_links(links))


def atlas_release(value):
    if not re.fullmatch(r"[1-9][0-9]*", value) or int(value) % 2:
        raise argparse.ArgumentTypeError("release must be a positive even Atlas release number")
    return value


def is_article_data_request(post_data, topic_id):
    try:
        message = json.loads(parse_qs(post_data, keep_blank_values=True)["message"][0])
    except (KeyError, IndexError, json.JSONDecodeError):
        return False
    return any(
        action.get("params", {}).get("classname") == "Help_ArticleDataController"
        and action.get("params", {}).get("method") == "getData"
        and action.get("params", {}).get("params", {}).get("articleParameters", {}).get("urlName") == topic_id
        for action in message.get("actions", [])
    )


def article_filename(topic_id):
    stem = topic_id.removeprefix("release-notes.").removesuffix(".htm")
    return re.sub(r"[^A-Za-z0-9_.-]", "_", stem) + ".md"


def build_article_request(template, topic_id, release):
    form = {key: values[0] for key, values in parse_qs(template, keep_blank_values=True).items()}
    message = json.loads(form["message"])
    params = message["actions"][0]["params"]["params"]["articleParameters"]
    params["urlName"] = topic_id
    params["release"] = f"{release}.0.0"
    form["message"] = json.dumps(message, separators=(",", ":"))
    form["aura.pageURI"] = "/s/articleView?" + urlencode(
        {"id": topic_id, "language": params.get("language", "en_US"), "release": release, "type": "5"}
    )
    return urlencode(form)


def needs_rendered_page(content):
    return content.startswith("Cannot populate due to large Document size - ")


def article_markdown(xhtml, parser="xml"):
    soup = BeautifulSoup(xhtml, parser)
    content = soup.select_one("#content") or soup.body
    if content is None:
        raise ValueError("article has no content body")
    for element in content.select(".slds-breadcrumb, .slds-assistive-text, script, style"):
        element.decompose()
    for link in content.find_all("a", href=True):
        topic = parse_qs(urlparse(link["href"]).query).get("id", [""])[0]
        if TOPIC_PATTERN.fullmatch(topic):
            link["href"] = article_filename(topic)
    text = markdownify(str(content), heading_style="ATX", bullets="-")
    text = re.sub(r"\n{3,}", "\n\n", text).strip()
    if not text:
        raise ValueError("article converted to empty Markdown")
    return text + "\n"


def extract_article(payload, topic_id, release):
    actions = payload.get("actions") or []
    if len(actions) != 1 or actions[0].get("state") != "SUCCESS":
        raise ValueError("article request did not succeed")
    record = (((actions[0].get("returnValue") or {}).get("returnValue") or {}).get("record") or {})
    if record.get("Is_Error_Response__c"):
        raise ValueError("article API returned an error record")
    expected_version = f"{release}.0.0"
    if record.get("Version__c") != expected_version:
        raise ValueError(f"article version {record.get('Version__c')!r} != {expected_version!r}")
    expected_topic = topic_id.removesuffix(".htm")
    if record.get("Topic_Id__c") != expected_topic:
        raise ValueError(f"article topic {record.get('Topic_Id__c')!r} != {expected_topic!r}")
    if not record.get("Content__c"):
        raise ValueError("article has no XHTML content")
    return record


def utc_timestamp():
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def write_atomic(path, data):
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(data, encoding="utf-8")
    os.replace(temporary, path)


def toc_metadata(release, source, entries):
    tool = Path(__file__).resolve()
    return {
        "schemaVersion": 1,
        "release": f"{release}.0.0",
        "source": source,
        "entries": entries,
        "tool": str(tool),
        "toolSha256": hashlib.sha256(tool.read_bytes()).hexdigest(),
    }


async def load_toc_and_template(page, release, language):
    root_url = (
        "https://help.salesforce.com/s/articleView?"
        + urlencode({"id": ROOT_TOPIC, "language": language, "release": release, "type": "5"})
    )
    loop = asyncio.get_running_loop()
    template_future = loop.create_future()

    def capture(request):
        post_data = request.post_data or ""
        if not is_article_data_request(post_data, ROOT_TOPIC):
            return
        if not template_future.done():
            headers = {
                key: value
                for key, value in request.headers.items()
                if key.lower()
                in {
                    "content-type",
                    "referer",
                    "x-sfdc-lds-endpoints",
                    "x-sfdc-page-cache",
                    "x-sfdc-page-scope-id",
                }
            }
            template_future.set_result((request.url, post_data, headers))

    page.on("request", capture)
    await page.goto(root_url, wait_until="domcontentloaded", timeout=120_000)
    # Page-wide release-note links include article/header duplicates before the navigation tree.
    await page.wait_for_selector(TOC_LINK_SELECTOR, timeout=120_000)
    endpoint, template, headers = await asyncio.wait_for(template_future, timeout=120)
    links = await page.locator(TOC_LINK_SELECTOR).evaluate_all("""
        els => els.map(a => ({
            href: a.href,
            text: (a.textContent || '').trim(),
            ariaLevel: a.closest('[aria-level]')?.getAttribute('aria-level') || '',
        }))
    """)
    entries = toc_entries_from_links(links)
    topics = topics_from_links(links)
    if ROOT_TOPIC not in topics:
        raise ValueError("seasonal table of contents is missing its root topic")
    return root_url, entries, topics, endpoint, template, headers


async def fetch_article(request_context, endpoint, template, headers, topic_id, release, attempts):
    body = build_article_request(template, topic_id, release)
    last_error = None
    for attempt in range(attempts):
        try:
            response = await request_context.post(endpoint, headers=headers, data=body, timeout=120_000)
            if response.status in {403, 429} or response.status >= 500:
                raise RuntimeError(f"HTTP {response.status}")
            if not response.ok:
                raise ValueError(f"HTTP {response.status}")
            return extract_article(await response.json(), topic_id, release)
        except Exception as error:
            last_error = error
            if attempt + 1 < attempts:
                await asyncio.sleep(2**attempt)
    raise RuntimeError(f"{topic_id}: {last_error}")


async def export(args):
    args.output.mkdir(parents=True, exist_ok=True)
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        context = await browser.new_context()
        page = await context.new_page()
        try:
            root_url, toc_entries, topics, endpoint, template, headers = await load_toc_and_template(
                page, args.release, args.language
            )
            if args.toc_only:
                metadata = toc_metadata(args.release, root_url, toc_entries)
                write_atomic(args.output / "_toc.json", json.dumps(metadata, indent=2, ensure_ascii=False) + "\n")
                print(json.dumps({"release": metadata["release"], "tocArticleCount": len(toc_entries)}, indent=2))
                return 0
            selected = topics[: args.limit] if args.limit else topics
            filenames = [article_filename(topic) for topic in selected]
            if len({name.casefold() for name in filenames}) != len(filenames):
                raise ValueError("seasonal topic IDs collide as local filenames")

            semaphore = asyncio.Semaphore(args.concurrency)
            completed = 0

            async def one(topic_id):
                nonlocal completed
                output = args.output / article_filename(topic_id)
                source_url = "https://help.salesforce.com/s/articleView?" + urlencode(
                    {"id": topic_id, "language": args.language, "release": args.release, "type": "5"}
                )
                try:
                    async with semaphore:
                        record = await fetch_article(
                            context.request, endpoint, template, headers, topic_id, args.release, args.attempts
                        )
                        acquisition = "article-api-xhtml"
                        if needs_rendered_page(record["Content__c"]):
                            fallback_page = await context.new_page()
                            try:
                                await fallback_page.goto(source_url, wait_until="domcontentloaded", timeout=120_000)
                                await fallback_page.wait_for_selector("#content h1", timeout=120_000)
                                html = await fallback_page.locator("#content").evaluate("element => element.outerHTML")
                            finally:
                                await fallback_page.close()
                            markdown = article_markdown(html, parser="lxml")
                            acquisition = "rendered-page"
                        else:
                            markdown = article_markdown(record["Content__c"])
                        write_atomic(output, markdown)
                        if args.request_delay:
                            await asyncio.sleep(args.request_delay)
                    return {
                        "topicId": topic_id,
                        "sourceUrl": source_url,
                        "sourceAcquisition": acquisition,
                        "file": output.name,
                        "title": record.get("Title__c", ""),
                        "version": record.get("Version__c", ""),
                        "publishedAt": record.get("Published_Date__c", ""),
                        "sha256": hashlib.sha256(markdown.encode()).hexdigest(),
                    }
                except Exception as error:
                    raise RuntimeError(f"{topic_id}: {error}") from error
                finally:
                    completed += 1
                    if completed % 50 == 0 or completed == len(selected):
                        print(f"exported {completed}/{len(selected)}", flush=True)

            results = await asyncio.gather(*(one(topic) for topic in selected), return_exceptions=True)
        finally:
            await browser.close()

    files = sorted((result for result in results if isinstance(result, dict)), key=lambda item: item["topicId"])
    failures = sorted(str(result) for result in results if isinstance(result, Exception))
    metadata = {
        "schemaVersion": 1,
        "source": root_url,
        "release": f"{args.release}.0.0",
        "language": args.language,
        "exportedAt": utc_timestamp(),
        "tocArticleCount": len(topics),
        "tocTopics": topics,
        "tocEntries": toc_entries,
        "selectedArticleCount": len(selected),
        "exportedArticleCount": len(files),
        "complete": not args.limit and not failures and len(files) == len(topics),
        "failures": failures,
        "acquisition": "Salesforce Help seasonal TOC and Help_ArticleDataController article XHTML",
        "tool": str(Path(__file__).resolve()),
        "toolSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "dependencies": {
            name: importlib.metadata.version(name)
            for name in ("playwright", "beautifulsoup4", "markdownify", "lxml")
        },
        "files": files,
    }
    write_atomic(args.output / "_export-meta.json", json.dumps(metadata, indent=2, ensure_ascii=False) + "\n")
    print(json.dumps({key: metadata[key] for key in ("release", "tocArticleCount", "exportedArticleCount", "complete", "failures")}, indent=2))
    return 0 if not failures and len(files) == len(selected) else 1


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--release", required=True, type=atlas_release)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--language", default="en_US")
    parser.add_argument("--concurrency", type=int, default=4)
    parser.add_argument("--request-delay", type=float, default=0.05)
    parser.add_argument("--attempts", type=int, default=3)
    parser.add_argument("--limit", type=int, default=0, help="Smoke-test only; zero exports the complete TOC")
    parser.add_argument("--toc-only", action="store_true", help="Write the seasonal TOC without fetching articles")
    args = parser.parse_args()
    if args.concurrency < 1 or args.attempts < 1 or args.limit < 0 or args.request_delay < 0:
        parser.error("numeric options must be non-negative and concurrency/attempts must be positive")
    return args


if __name__ == "__main__":
    raise SystemExit(asyncio.run(export(parse_args())))
