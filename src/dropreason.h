/*
 * Copyright 2014-2018,2023,2025-2026 Con Kolivas
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License as published by the Free
 * Software Foundation; either version 3 of the License, or (at your option)
 * any later version.  See COPYING for more details.
 */

#ifndef DROPREASON_H
#define DROPREASON_H

/* Why a client connection was closed. The numeric values travel between the
 * connector and stratifier processes in the dropclient message, so new
 * reasons are only ever appended before DR_MAX. Consumers outside the daemon
 * see the string form from drop_reason_str(). */
enum drop_reason {
	DR_OTHER = 0,
	DR_PEER_EOF,
	DR_PEER_RESET,
	DR_EPOLLHUP,
	DR_EPOLLRDHUP,
	DR_EPOLLERR,
	DR_BAD_JSON,
	DR_MSG_OVERSIZE,
	DR_BUF_OVERLOAD,
	DR_SV2_PROTO,
	DR_SV2_ENCRYPT,
	DR_NOT_SUBSCRIBED,
	DR_ROLE_REFUSED,
	DR_WRITE_ERR,
	DR_BLOCKED_60S,
	DR_SENDQ_LIMIT,
	DR_PRE_SESSION_IDLE,
	DR_AUTH_TIMEOUT,
	DR_IDLE_DROP,
	DR_INVALID_SHARES,
	DR_UPSTREAM_REJECT,
	DR_AUTH_REJECTED_UPSTREAM,
	DR_RECONNECT_IGNORED,
	DR_DROPALL,
	DR_MINING_TERM,
	DR_SMSG_ERR,
	DR_MAX
};

static inline const char *drop_reason_str(int r)
{
	static const char *const names[DR_MAX] = {
		"other",
		"peer_eof",
		"peer_reset",
		"epollhup",
		"epollrdhup",
		"epollerr",
		"bad_json",
		"msg_oversize",
		"buf_overload",
		"sv2_proto",
		"sv2_encrypt",
		"not_subscribed",
		"role_refused",
		"write_err",
		"blocked_60s",
		"sendq_limit",
		"pre_session_idle",
		"auth_timeout",
		"idle_drop",
		"invalid_shares",
		"upstream_reject",
		"auth_rejected_upstream",
		"reconnect_ignored",
		"dropall",
		"mining_term",
		"smsg_err"
	};

	if (r < 0 || r >= DR_MAX)
		return "other";
	return names[r];
}

#endif /* DROPREASON_H */
