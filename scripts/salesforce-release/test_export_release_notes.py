import json
import sys
import unittest
import warnings
from pathlib import Path
from urllib.parse import parse_qs, urlencode
from unittest.mock import patch

import export_release_notes as exporter
from bs4 import XMLParsedAsHTMLWarning


class ExportReleaseNotesTests(unittest.TestCase):
    def test_article_data_request_requires_get_data_action(self):
        page_uri_only = urlencode({
            "message": json.dumps({"actions": [{"params": {
                "classname": "Help_ArticleDataController",
                "method": "getProductViewableFlag",
                "params": {"productType": "release-notes"},
            }}]}),
            "aura.pageURI": f"/s/articleView?id={exporter.ROOT_TOPIC}",
        })
        get_data = urlencode({
            "message": json.dumps({"actions": [{"params": {
                "classname": "Help_ArticleDataController",
                "method": "getData",
                "params": {"articleParameters": {"urlName": exporter.ROOT_TOPIC}},
            }}]}),
            "aura.pageURI": f"/s/articleView?id={exporter.ROOT_TOPIC}",
        })

        self.assertFalse(exporter.is_article_data_request(page_uri_only, exporter.ROOT_TOPIC))
        self.assertTrue(exporter.is_article_data_request(get_data, exporter.ROOT_TOPIC))

    def test_topics_from_links_keeps_unique_official_release_note_ids(self):
        links = [
            {"href": "https://help.salesforce.com/s/articleView?id=release-notes.rn_apex.htm&release=260&type=5", "text": "Apex"},
            {"href": "/s/articleView?id=release-notes.rn_apex.htm&release=260&type=5#anchor", "text": "Apex duplicate"},
            {"href": "/s/articleView?id=release-notes.rn_lwc.htm&release=260&type=5", "text": "LWC"},
            {"href": "/s/articleView?id=release-notes.salesforce_help_map.htm&release=260&type=5", "text": "Docs home"},
            {"href": "/s/articleView?id=other.guide.htm&release=260&type=5", "text": "Other"},
        ]

        self.assertEqual(
            exporter.topics_from_links(links),
            ["release-notes.rn_apex.htm", "release-notes.rn_lwc.htm"],
        )

    def test_toc_entries_keep_dom_order_titles_and_ancestor_titles(self):
        links = [
            {"href": "/s/articleView?id=release-notes.salesforce_release_notes.htm", "text": "Summer '26 Release Notes", "ariaLevel": "1"},
            {"href": "/s/articleView?id=release-notes.rn_apex.htm", "text": "  Apex  ", "ariaLevel": "2"},
            {"href": "/s/articleView?id=release-notes.rn_apex_new.htm", "text": "New Apex", "ariaLevel": "3"},
            {"href": "/s/articleView?id=release-notes.rn_apex.htm", "text": "Apex duplicate", "ariaLevel": "2"},
            {"href": "/s/articleView?id=other.guide.htm", "text": "Ignored", "ariaLevel": "2"},
            {"href": "/s/articleView?id=release-notes.rn_lwc.htm", "text": "LWC", "ariaLevel": "2"},
        ]

        self.assertEqual(
            exporter.toc_entries_from_links(links),
            [
                {
                    "topicId": "release-notes.salesforce_release_notes.htm",
                    "title": "Summer '26 Release Notes",
                    "ancestorTitles": [],
                    "ancestorTopicIds": [],
                },
                {
                    "topicId": "release-notes.rn_apex.htm",
                    "title": "Apex",
                    "ancestorTitles": ["Summer '26 Release Notes"],
                    "ancestorTopicIds": ["release-notes.salesforce_release_notes.htm"],
                },
                {
                    "topicId": "release-notes.rn_apex_new.htm",
                    "title": "New Apex",
                    "ancestorTitles": ["Summer '26 Release Notes", "Apex"],
                    "ancestorTopicIds": [
                        "release-notes.salesforce_release_notes.htm",
                        "release-notes.rn_apex.htm",
                    ],
                },
                {
                    "topicId": "release-notes.rn_lwc.htm",
                    "title": "LWC",
                    "ancestorTitles": ["Summer '26 Release Notes"],
                    "ancestorTopicIds": ["release-notes.salesforce_release_notes.htm"],
                },
            ],
        )

    def test_toc_entries_use_tree_records_not_pagewide_duplicate_links(self):
        outside_links = [
            {"href": "/s/articleView?id=release-notes.rn_apex.htm", "text": "Apex article link", "ariaLevel": "1"},
        ]
        toc_links = [
            {"href": "/s/articleView?id=release-notes.salesforce_release_notes.htm", "text": "Summer '26 Release Notes", "ariaLevel": "2"},
            {"href": "/s/articleView?id=release-notes.rn_apex.htm", "text": "Apex", "ariaLevel": "3"},
        ]

        self.assertEqual(len(outside_links), 1)
        self.assertEqual(
            exporter.TOC_LINK_SELECTOR,
            'nav.toc-container li[aria-level] a[href*="id=release-notes."]',
        )
        self.assertEqual(
            exporter.toc_entries_from_links(toc_links),
            [
                {
                    "topicId": "release-notes.salesforce_release_notes.htm",
                    "title": "Summer '26 Release Notes",
                    "ancestorTitles": [],
                    "ancestorTopicIds": [],
                },
                {
                    "topicId": "release-notes.rn_apex.htm",
                    "title": "Apex",
                    "ancestorTitles": ["Summer '26 Release Notes"],
                    "ancestorTopicIds": ["release-notes.salesforce_release_notes.htm"],
                },
            ],
        )

    def test_atlas_release_requires_a_positive_even_number(self):
        self.assertEqual(exporter.atlas_release("258"), "258")
        self.assertEqual(exporter.atlas_release("260"), "260")
        self.assertEqual(exporter.atlas_release("262"), "262")
        for release in ("0", "261", "-2", "260.0", "0260"):
            with self.subTest(release=release):
                with self.assertRaises(exporter.argparse.ArgumentTypeError):
                    exporter.atlas_release(release)

    def test_toc_only_arguments_accept_future_atlas_release(self):
        with patch.object(sys, "argv", ["export_release_notes.py", "--release", "264", "--output", "out", "--toc-only"]):
            args = exporter.parse_args()

        self.assertEqual(args.release, "264")
        self.assertTrue(args.toc_only)

    def test_toc_metadata_records_source_entries_and_tool_identity(self):
        entries = [{
            "topicId": "release-notes.rn_apex.htm",
            "title": "Apex",
            "ancestorTitles": [],
            "ancestorTopicIds": [],
        }]

        metadata = exporter.toc_metadata("262", "https://example.test/toc", entries)

        self.assertEqual(metadata["release"], "262.0.0")
        self.assertEqual(metadata["source"], "https://example.test/toc")
        self.assertEqual(metadata["entries"], entries)
        self.assertEqual(metadata["tool"], str(Path(exporter.__file__).resolve()))
        self.assertRegex(metadata["toolSha256"], r"^[0-9a-f]{64}$")

    def test_build_article_request_rebinds_only_article_identity(self):
        message = {
            "actions": [{
                "params": {
                    "params": {
                        "articleParameters": {
                            "urlName": "release-notes.salesforce_release_notes.htm",
                            "language": "en_US",
                            "release": "260.0.0",
                            "requestedArticleType": "HelpDocs",
                            "requestedArticleTypeNumber": "5",
                        }
                    }
                }
            }]
        }
        template = urlencode({
            "message": json.dumps(message),
            "aura.context": '{"mode":"PROD"}',
            "aura.pageURI": "/s/articleView?id=release-notes.salesforce_release_notes.htm&release=260&type=5",
            "aura.token": "null",
        })

        rebound = parse_qs(exporter.build_article_request(template, "release-notes.rn_apex.htm", "262"))
        rebound_message = json.loads(rebound["message"][0])
        params = rebound_message["actions"][0]["params"]["params"]["articleParameters"]

        self.assertEqual(params["urlName"], "release-notes.rn_apex.htm")
        self.assertEqual(params["release"], "262.0.0")
        self.assertIn("id=release-notes.rn_apex.htm", rebound["aura.pageURI"][0])
        self.assertIn("release=262", rebound["aura.pageURI"][0])
        self.assertEqual(rebound["aura.context"], ['{"mode":"PROD"}'])

    def test_article_markdown_uses_content_body_and_relative_release_links(self):
        xhtml = """<html><head><title>Ignore</title></head><body>
        <div id="content"><ol class="slds-breadcrumb"><li>You are here</li></ol>
        <h1>Apex Change</h1><p>Use <a href="/apex/HTViewHelpDoc?id=release-notes.rn_lwc.htm">LWC</a>.</p>
        </div></body></html>"""

        markdown = exporter.article_markdown(xhtml)

        self.assertIn("# Apex Change", markdown)
        self.assertIn("[LWC](rn_lwc.md)", markdown)
        self.assertNotIn("You are here", markdown)

    def test_article_markdown_parses_official_xhtml_without_html_warning(self):
        xhtml = """<?xml version="1.0" encoding="UTF-8"?>
        <concept xml:lang="en-us"><div id="content"><h1>Apex</h1></div></concept>"""

        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always")
            exporter.article_markdown(xhtml)

        self.assertFalse(any(issubclass(item.category, XMLParsedAsHTMLWarning) for item in caught))

    def test_large_article_sentinel_requires_rendered_page_fallback(self):
        self.assertTrue(
            exporter.needs_rendered_page("Cannot populate due to large Document size - 260927 characters.")
        )
        self.assertFalse(exporter.needs_rendered_page("<concept><div id='content'>Apex</div></concept>"))

    def test_rendered_article_html_uses_html_parser(self):
        html = """<div id="content"><h1>Release Note Changes</h1>
        <p>See <a href="/s/articleView?id=release-notes.rn_apex.htm&amp;release=260">Apex</a>.</p>
        <br></div>"""

        markdown = exporter.article_markdown(html, parser="lxml")

        self.assertIn("# Release Note Changes", markdown)
        self.assertIn("[Apex](rn_apex.md)", markdown)

    def test_extract_article_rejects_wrong_release(self):
        response = {
            "actions": [{
                "state": "SUCCESS",
                "returnValue": {"returnValue": {"record": {
                    "Topic_Id__c": "release-notes.rn_apex",
                    "Title__c": "Apex",
                    "Version__c": "260.0.0",
                    "Content__c": "<html><body><div id='content'><h1>Apex</h1></div></body></html>",
                    "Is_Error_Response__c": False,
                }}}
            }]
        }

        with self.assertRaisesRegex(ValueError, "version"):
            exporter.extract_article(response, "release-notes.rn_apex.htm", "262")


if __name__ == "__main__":
    unittest.main()
