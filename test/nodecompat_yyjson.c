/*
 * yyjson for the nodecompat test. It is linked into the daemon rather than
 * libckpool.a; its own unit keeps yyjson's internal helpers apart from
 * libckpool.h, and a distinct basename keeps the object apart from the
 * daemon's src/yyjson.o under subdir-objects.
 */

#include "../src/yyjson.c"
