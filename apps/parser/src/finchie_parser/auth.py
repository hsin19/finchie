"""Who may call the parser: with a token set, only a caller that sends it (ADR 0002).

The parser opens statements with the owner's passwords and details, so anything that reaches its port could
have one decrypted. The private network is the first wall; the token is the second. Health checks and
reflection need none: they reveal nothing.
"""

import hmac
from collections.abc import Callable
from typing import Any

import grpc

from finchie.parser.v2 import parser_pb2

_GUARDED = "/" + parser_pb2.DESCRIPTOR.services_by_name["ParserService"].full_name + "/"


class TokenInterceptor(grpc.ServerInterceptor):  # type: ignore[misc]  # grpc ships no types for it
    def __init__(self, token: str) -> None:
        self._expected = f"Bearer {token}".encode()

    def intercept_service(
        self,
        continuation: Callable[[grpc.HandlerCallDetails], grpc.RpcMethodHandler | None],
        handler_call_details: grpc.HandlerCallDetails,
    ) -> grpc.RpcMethodHandler | None:
        method: str = handler_call_details.method
        if not method.startswith(_GUARDED):
            return continuation(handler_call_details)
        sent = dict(handler_call_details.invocation_metadata or ()).get("authorization", "")
        if hmac.compare_digest(str(sent).encode(), self._expected):
            return continuation(handler_call_details)
        return grpc.unary_unary_rpc_method_handler(_refuse)


def _refuse(_request: Any, context: grpc.ServicerContext) -> None:
    context.abort(grpc.StatusCode.UNAUTHENTICATED, "a token is required")
