/* SpeechKit portability shims for the vendored phonon-2-c sources: aligned
 * allocation, the CPU count and a read-only file mapping on Windows (MinGW),
 * the libc calls elsewhere. Not part of upstream phonon-2-c. */
#ifndef P2_COMPAT_H
#define P2_COMPAT_H

#include <stddef.h>
#include <stdlib.h>

#ifdef _WIN32
#include <malloc.h>
#include <windows.h>
#include <io.h>

static inline void *p2_aligned_alloc(size_t align, size_t size) { return _aligned_malloc(size, align); }
static inline void p2_aligned_free(void *p) { _aligned_free(p); }
static inline long p2_online_cpus(void) { return (long)GetActiveProcessorCount(ALL_PROCESSOR_GROUPS); }

#define P2_MAP_FAILED ((void *)-1)
static inline void *p2_map_readonly(int fd, size_t len)
{
    HANDLE m = CreateFileMappingA((HANDLE)_get_osfhandle(fd), NULL, PAGE_READONLY, 0, 0, NULL);
    if (!m) return P2_MAP_FAILED;
    void *p = MapViewOfFile(m, FILE_MAP_READ, 0, 0, len);
    CloseHandle(m);
    return p ? p : P2_MAP_FAILED;
}
static inline void p2_unmap(void *p, size_t len) { (void)len; UnmapViewOfFile(p); }
#else
#include <sys/mman.h>
#include <unistd.h>

#if defined(__ANDROID__) && __ANDROID_API__ < 28
/* Bionic declares aligned_alloc from API level 28; the Android app supports 26. */
static inline void *p2_aligned_alloc(size_t align, size_t size)
{
    void *p = NULL;
    return posix_memalign(&p, align, size) == 0 ? p : NULL;
}
#else
static inline void *p2_aligned_alloc(size_t align, size_t size) { return aligned_alloc(align, size); }
#endif
static inline void p2_aligned_free(void *p) { free(p); }
static inline long p2_online_cpus(void) { return sysconf(_SC_NPROCESSORS_ONLN); }

#define P2_MAP_FAILED MAP_FAILED
static inline void *p2_map_readonly(int fd, size_t len) { return mmap(NULL, len, PROT_READ, MAP_PRIVATE, fd, 0); }
static inline void p2_unmap(void *p, size_t len) { munmap(p, len); }
#endif

#endif /* P2_COMPAT_H */
