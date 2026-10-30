/*
 * Copyright 2026 Con Kolivas
 *
 * Stand-alone SV2 Noise responder for interoperability checks.
 *
 * Serves the server side of the Noise_NX handshake from sv2_conn.c on a
 * loopback TCP socket so that an independent initiator implementation can be
 * pointed at it.
 *
 *   sv2_interop_responder [-p PORT] [-n COUNT] [-V CERT_VERSION]
 *                         [-a AUTHORITY_KEY_FILE] [-s STATIC_KEY_FILE]
 *
 * stdout carries exactly one line, written once the listener is up:
 *   stratum2+tcp://127.0.0.1:<port>/<authority base58check>
 * Everything else, including the library's own log output, goes to stderr.
 *
 * -V replaces cert_version on a private copy of the server keys, producing a
 * correctly signed certificate that an initiator must reject.
 *
 * Exit status: 0 after COUNT connections, 1 if any handshake failed,
 * 2 on usage, key or socket setup errors.
 */

#include "config.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <unistd.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <netinet/in.h>
#include <arpa/inet.h>

#include "libckpool.h"
#include "sv2_conn.h"

static bool write_all(int fd, const uint8_t *buf, size_t len)
{
	while (len) {
		ssize_t n = write(fd, buf, len);

		if (n < 0) {
			if (errno == EINTR)
				continue;
			return false;
		}
		buf += n;
		len -= (size_t)n;
	}
	return true;
}

/* Serve one connection; returns false if the handshake layer reported a
 * fatal error. */
static bool serve(int fd, const struct sv2_noise_server_keys *keys)
{
	struct sv2_conn *c = sv2_conn_new(keys);
	uint8_t buf[4096];
	bool ok = true;

	if (!c) {
		fprintf(stderr, "sv2_conn_new failed\n");
		return false;
	}
	for (;;) {
		uint8_t *reply = NULL, **frames = NULL;
		size_t reply_len = 0, *frame_lens = NULL, nframes = 0, i;
		ssize_t n = read(fd, buf, sizeof(buf));
		bool fed;

		if (n < 0 && errno == EINTR)
			continue;
		if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
			fprintf(stderr, "read timeout\n");
			ok = false;
			break;
		}
		if (n <= 0) {
			fprintf(stderr, "peer closed\n");
			break;
		}
		fprintf(stderr, "read %zd bytes\n", n);
		fed = sv2_conn_feed(c, buf, (size_t)n, &reply, &reply_len,
				    &frames, &frame_lens, &nframes);
		if (fed && reply && reply_len) {
			if (write_all(fd, reply, reply_len) && sv2_conn_ready(c))
				fprintf(stderr, "act2 sent (%zu bytes)\n", reply_len);
		}
		dealloc(reply);
		for (i = 0; i < nframes; i++)
			dealloc(frames[i]);
		dealloc(frames);
		dealloc(frame_lens);
		if (!fed) {
			fprintf(stderr, "sv2_conn_feed failed\n");
			ok = false;
			break;
		}
	}
	sv2_conn_free(c);
	return ok;
}

static void usage(void)
{
	fprintf(stderr, "usage: sv2_interop_responder [-p PORT] [-n COUNT] "
		"[-V CERT_VERSION] [-a AUTHORITY_KEY_FILE] [-s STATIC_KEY_FILE]\n");
}

int main(int argc, char **argv)
{
	const char *authority_path = NULL, *static_path = NULL;
	struct sv2_noise_server_keys keys;
	struct sockaddr_in addr;
	struct timeval tv;
	socklen_t alen = sizeof(addr);
	int port = 0, count = 1, version = -1, opt, lfd, i, rc = 0;
	FILE *urlout;
	char *b58;

	while ((opt = getopt(argc, argv, "p:n:V:a:s:")) != -1) {
		switch (opt) {
		case 'p':
			port = atoi(optarg);
			break;
		case 'n':
			count = atoi(optarg);
			break;
		case 'V':
			version = atoi(optarg);
			break;
		case 'a':
			authority_path = optarg;
			break;
		case 's':
			static_path = optarg;
			break;
		default:
			usage();
			return 2;
		}
	}
	if (optind != argc || port < 0 || port > 65535 || count < 1 ||
	    (version != -1 && (version < 0 || version > 65535))) {
		usage();
		return 2;
	}

	/* The library logs to stdout; keep the real stdout for the URL line
	 * only and send everything else to stderr. */
	i = dup(STDOUT_FILENO);
	urlout = i >= 0 ? fdopen(i, "w") : NULL;
	if (!urlout || dup2(STDERR_FILENO, STDOUT_FILENO) < 0) {
		fprintf(stderr, "cannot redirect stdout\n");
		return 2;
	}

	if (!sv2_init_server_keys(authority_path, static_path)) {
		fprintf(stderr, "sv2_init_server_keys failed\n");
		return 2;
	}
	keys = *sv2_get_server_keys();
	if (version >= 0)
		keys.cert_version = (uint16_t)version;

	lfd = socket(AF_INET, SOCK_STREAM, 0);
	if (lfd < 0) {
		perror("socket");
		return 2;
	}
	i = 1;
	setsockopt(lfd, SOL_SOCKET, SO_REUSEADDR, &i, sizeof(i));
	memset(&addr, 0, sizeof(addr));
	addr.sin_family = AF_INET;
	addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
	addr.sin_port = htons((uint16_t)port);
	if (bind(lfd, (struct sockaddr *)&addr, sizeof(addr)) < 0 ||
	    listen(lfd, 8) < 0 ||
	    getsockname(lfd, (struct sockaddr *)&addr, &alen) < 0) {
		perror("bind/listen");
		return 2;
	}

	b58 = sv2_noise_authority_pubkey_b58(&keys);
	if (!b58) {
		fprintf(stderr, "authority base58check encode failed\n");
		return 2;
	}
	fprintf(urlout, "stratum2+tcp://127.0.0.1:%u/%s\n",
		ntohs(addr.sin_port), b58);
	fflush(urlout);
	dealloc(b58);

	for (i = 0; i < count; i++) {
		int fd = accept(lfd, NULL, NULL);

		if (fd < 0) {
			if (errno == EINTR) {
				i--;
				continue;
			}
			perror("accept");
			return 2;
		}
		fprintf(stderr, "accepted\n");
		/* A stalled initiator must not hold the responder open past
		 * the wrapper's own timeout. */
		tv.tv_sec = 5;
		tv.tv_usec = 0;
		setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
		if (!serve(fd, &keys))
			rc = 1;
		close(fd);
	}
	close(lfd);
	return rc;
}
