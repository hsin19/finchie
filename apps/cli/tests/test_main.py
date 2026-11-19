import json
import re
from pathlib import Path

from conftest import Answer, Cli, FakeAPI, Request, json_answer


def test_help_lists_every_command_with_its_method(cli: Cli) -> None:
    run = cli("--help")
    assert run.code == 0
    assert re.search(r"^  list-things +GET +Things, a page at a time$", run.text, re.MULTILINE)
    assert re.search(r"^  create-thing +POST +A thing of the owner's$", run.text, re.MULTILINE)
    assert "get-thing-pdf" in run.text
    assert cli().text == run.text


def test_command_help_spells_out_its_options_and_the_fields_it_returns(cli: Cli) -> None:
    run = cli("list-things", "--help")
    assert run.code == 0
    assert "GET /api/things" in run.text
    assert "--kind VALUE" in run.text
    assert "One of: a, b. Repeat it for more than one" in run.text
    assert "--open, --no-open" in run.text
    assert "Only open ones" in run.text
    assert "Default 2. At most 2" in run.text
    assert "--all" in run.text
    assert "array of Thing" in run.text
    assert "amount: Decimal (string) — What it cost" in run.text
    assert "kind?: Kind (a | b) — a: one kind. b: the other" in run.text
    assert "note?: string, nullable" in run.text
    # A schema is spelled out once; inside itself it is named only.
    assert "parts?: array of Thing (as above)" in run.text


def test_help_of_a_command_with_a_path_parameter_and_errors(cli: Cli) -> None:
    run = cli("get-thing", "--help")
    assert run.text.startswith("finchie get-thing <id> [options]")
    assert "404 Error" in run.text


def test_sends_the_service_token_with_every_request(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/things/7")] = lambda _: json_answer({"id": 7, "amount": "1.50"})
    run = cli("get-thing", "7", env={"FINCHIE_CLIENT_ID": "agent.access", "FINCHIE_CLIENT_SECRET": "s3cret"})
    assert run.code == 0, run.stderr
    assert len(api.requests) == 2
    for request in api.requests:
        assert request.headers["cf-access-client-id"] == "agent.access"
        assert request.headers["cf-access-client-secret"] == "s3cret"


def test_sends_no_token_without_one(api: FakeAPI, cli: Cli) -> None:
    cli("--help")
    assert "cf-access-client-id" not in api.requests[0].headers


def test_options_become_the_query_the_contract_describes(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/things")] = lambda _: json_answer([])
    assert cli("list-things", "--kind", "a", "--kind", "b", "--open", "--limit", "1").code == 0
    assert cli("list-things", "--no-open").code == 0
    first, second = api.calls()
    assert first.query == [("limit", "1"), ("kind", "a"), ("kind", "b"), ("open", "true")]
    assert second.query == [("open", "false")]


def test_path_parameters_come_first_and_by_the_operation_id_too(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/things/7")] = lambda _: json_answer({"id": 7, "amount": "1.50"})
    assert cli("get-thing", "7").code == 0
    assert cli("getThing", "7").code == 0
    assert [r.path for r in api.calls()] == ["/api/things/7", "/api/things/7"]


def test_a_value_the_contract_does_not_allow_is_a_usage_error(api: FakeAPI, cli: Cli) -> None:
    run = cli("list-things", "--kind", "c")
    assert run.code == 2
    assert run.stderr.startswith("finchie list-things: argument --kind: invalid choice: 'c'")
    assert run.stderr.rstrip().endswith("(see finchie list-things --help)")
    assert api.calls() == []


def test_an_unknown_command_is_a_usage_error(cli: Cli) -> None:
    run = cli("list-everything")
    assert run.code == 2
    assert "no command 'list-everything'" in run.stderr


def test_all_goes_through_the_pages_by_offset(api: FakeAPI, cli: Cli) -> None:
    def page(request: Request) -> Answer:
        offset = int(dict(request.query).get("offset", "0"))
        return json_answer([{"id": i, "amount": "1"} for i in range(offset, min(offset + 2, 5))])

    api.routes[("GET", "/api/things")] = page
    run = cli("list-things", "--kind", "a", "--all")
    assert run.code == 0, run.stderr
    assert [json.loads(line)["id"] for line in run.text.splitlines()] == [0, 1, 2, 3, 4]
    # The largest page the contract allows, the filter kept on every page.
    assert [sorted(r.query) for r in api.calls()] == [
        [("kind", "a"), ("limit", "2")],
        [("kind", "a"), ("limit", "2"), ("offset", "2")],
        [("kind", "a"), ("limit", "2"), ("offset", "4")],
    ]


def test_all_goes_on_before_the_last_item_of_each_page(api: FakeAPI, cli: Cli) -> None:
    ids = [9, 8, 7, 6]

    def page(request: Request) -> Answer:
        before = int(dict(request.query).get("before", "100"))
        return json_answer({"items": [{"id": i} for i in ids if i < before][:2]})

    api.routes[("GET", "/api/docs")] = page
    run = cli("list-docs", "--all")
    assert [json.loads(line)["id"] for line in run.text.splitlines()] == [9, 8, 7, 6]
    assert [dict(r.query).get("before") for r in api.calls()] == [None, "8", "6"]


def test_all_needs_a_page_of_at_least_one(cli: Cli) -> None:
    assert cli("list-things", "--limit", "0", "--all").code == 2


def test_json_is_compact_unless_on_a_terminal(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/things/7")] = lambda _: json_answer({"id": 7, "note": "早餐"})
    assert cli("get-thing", "7").text == '{"id":7,"note":"早餐"}\n'
    assert cli("get-thing", "7", tty=True).text == '{\n  "id": 7,\n  "note": "早餐"\n}\n'


def test_an_error_from_the_api_goes_to_stderr_with_exit_status_1(cli: Cli) -> None:
    run = cli("get-thing", "9")
    assert run.code == 1
    assert run.stdout == b""
    assert run.stderr == 'HTTP 404: {"message": "no such route"}\n'


def test_a_redirect_means_access_refused_the_service_token(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/openapi.json")] = lambda _: Answer(
        status=302, content_type="text/html", headers={"Location": "https://team.cloudflareaccess.com/login"}
    )
    run = cli("--help")
    assert run.code == 1
    assert "Cloudflare Access refused the request" in run.stderr
    assert len(api.requests) == 1


def test_a_backend_without_the_contract_says_so(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/openapi.json")] = lambda _: json_answer({"message": "Not Found"}, 404)
    run = cli("--help")
    assert run.code == 1
    assert "is the backend older than the CLI?" in run.stderr

    api.routes[("GET", "/api/openapi.json")] = lambda _: Answer(content_type="text/html", body=b"<!doctype html>")
    assert "is the backend older than the CLI?" in cli("--help").stderr


def test_the_body_comes_as_given_from_a_file_or_from_stdin(api: FakeAPI, cli: Cli, tmp_path: Path) -> None:
    api.routes[("POST", "/api/things")] = lambda _: Answer(status=204)
    file = tmp_path / "thing.json"
    file.write_text('{"id": 2, "amount": "3"}')
    assert cli("create-thing", "--body", '{"id": 1, "amount": "2"}').code == 0
    assert cli("create-thing", "--body", f"@{file}").code == 0
    assert cli("create-thing", "--body", "-", stdin=b'{"id": 3, "amount": "4"}').code == 0
    assert [json.loads(r.body)["id"] for r in api.calls()] == [1, 2, 3]
    assert all(r.headers["content-type"] == "application/json" for r in api.calls())


def test_a_body_that_is_not_json_or_missing_is_a_usage_error(api: FakeAPI, cli: Cli) -> None:
    assert cli("create-thing", "--body", "{not json").code == 2
    assert cli("create-thing").code == 2
    assert api.calls() == []


def test_a_file_comes_as_its_bytes_but_never_onto_a_terminal(api: FakeAPI, cli: Cli) -> None:
    api.routes[("GET", "/api/things/7/file")] = lambda _: Answer(content_type="application/pdf", body=b"%PDF-1.7")
    assert cli("get-thing-pdf", "7").stdout == b"%PDF-1.7"
    run = cli("get-thing-pdf", "7", tty=True)
    assert run.code == 1
    assert run.stdout == b""
    assert "redirect stdout to a file" in run.stderr
