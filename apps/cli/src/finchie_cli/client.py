"""HTTP to the backend, through Cloudflare Access with a service token when one is set."""

import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from email.message import Message
from typing import IO

from finchie_cli.config import Settings
from finchie_cli.contract import json_media

TIMEOUT_SECONDS = 60


class CallError(Exception):
    """The call got no usable answer from the API."""


@dataclass(frozen=True)
class Reply:
    status: int
    content_type: str
    body: bytes

    @property
    def is_json(self) -> bool:
        return json_media(self.content_type)


class ApiError(Exception):
    """The API answered with an error; the reply holds its Problem."""

    def __init__(self, reply: Reply) -> None:
        super().__init__(f"HTTP {reply.status}")
        self.reply = reply


class _NoRedirects(urllib.request.HTTPRedirectHandler):
    # Access answers a refused service token with a redirect to its sign-in page; followed, it would
    # look like a page of HTML from the API.
    def redirect_request(
        self, req: urllib.request.Request, fp: IO[bytes], code: int, msg: str, headers: Message, newurl: str
    ) -> None:
        return None


class Client:
    def __init__(self, settings: Settings) -> None:
        self._settings = settings
        self._opener = urllib.request.build_opener(_NoRedirects())

    def send(
        self,
        method: str,
        path: str,
        query: Mapping[str, Sequence[str]] | None = None,
        body: bytes | None = None,
    ) -> Reply:
        url = self._settings.url + path
        if query:
            url += "?" + urllib.parse.urlencode([(k, v) for k, values in query.items() for v in values])
        headers = {"User-Agent": "finchie-cli", "Accept": "application/json, */*;q=0.5"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        if self._settings.client_id and self._settings.client_secret:
            headers["CF-Access-Client-Id"] = self._settings.client_id
            headers["CF-Access-Client-Secret"] = self._settings.client_secret
        request = urllib.request.Request(url, data=body, method=method, headers=headers)
        try:
            with self._opener.open(request, timeout=TIMEOUT_SECONDS) as response:
                return Reply(response.status, response.headers.get("Content-Type", ""), response.read())
        except urllib.error.HTTPError as e:
            if 300 <= e.code < 400:
                raise CallError(
                    f"{self._settings.url} redirected to {e.headers.get('Location', 'elsewhere')}: Cloudflare Access "
                    "refused the request. Check client_id and client_secret, and that the service token is in the "
                    "Access application's Service Auth policy"
                ) from e
            raise ApiError(Reply(e.code, e.headers.get("Content-Type", ""), e.read())) from e
        except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
            reason = e.reason if isinstance(e, urllib.error.URLError) else e
            raise CallError(f"cannot reach {self._settings.url}: {reason}") from e
