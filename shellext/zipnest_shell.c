/*
 * ZipNest shell extension: the "ZipNest" submenu in the Windows 11 File
 * Explorer context menu (IExplorerCommand). It is registered through a small
 * sparse MSIX package (see ../installer/msix) and simply starts ZipNest.exe
 * from the same folder with the right command-line switches.
 *
 * Build: x86_64-w64-mingw32-gcc -shared -O2 -o ZipNestShell.dll zipnest_shell.c zipnest_shell.def \
 *          -lole32 -luuid -lshlwapi -lshell32 -ladvapi32 -static-libgcc
 */
#define COBJMACROS
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <initguid.h>
#include <shobjidl.h>
#include <shlwapi.h>
#include <wchar.h>

/* {6F3A2C51-9B7E-4D2A-8C61-3E5B7A9D2F14} - must match AppxManifest.xml */
DEFINE_GUID(CLSID_ZipNestMenu, 0x6f3a2c51, 0x9b7e, 0x4d2a, 0x8c, 0x61, 0x3e, 0x5b, 0x7a, 0x9d, 0x2f, 0x14);

static HINSTANCE g_module;
static LONG g_objects;

enum Kind {
    K_ROOT, K_OPEN, K_EXTRACT_DLG, K_EXTRACT_SUB, K_EXTRACT_HERE, K_TEST, K_CONVERT, K_ADD, K_ADD_ZIP, K_COUNT
};
static const enum Kind SUBMENU[] = {K_OPEN, K_EXTRACT_DLG, K_EXTRACT_SUB, K_EXTRACT_HERE, K_TEST, K_CONVERT, K_ADD, K_ADD_ZIP};
#define SUBMENU_LEN (sizeof(SUBMENU) / sizeof(SUBMENU[0]))

static const wchar_t *ARCHIVE_EXT[] = {
    L".nest", L".zip", L".zipx", L".rar", L".7z", L".tar", L".gz", L".tgz", L".bz2", L".tbz2", L".tbz",
    L".xz", L".txz", L".zst", L".tzst", L".jar", L".cbz", L".cbr", L".cb7", L".001", NULL
};

/* ------------------------------------------------------------------ helpers */

static void exe_path(wchar_t *buf, DWORD cap) {
    GetModuleFileNameW(g_module, buf, cap);
    PathRemoveFileSpecW(buf);
    PathAppendW(buf, L"ZipNest.exe");
}

static BOOL is_archive(const wchar_t *path) {
    const wchar_t *ext = PathFindExtensionW(path);
    if (!ext || !*ext) return FALSE;
    for (int i = 0; ARCHIVE_EXT[i]; i++)
        if (_wcsicmp(ext, ARCHIVE_EXT[i]) == 0) return TRUE;
    /* name.r00 .. name.r99 (old multi-volume RAR) */
    if (wcslen(ext) == 4 && (ext[1] == L'r' || ext[1] == L'R') && iswdigit(ext[2]) && iswdigit(ext[3])) return TRUE;
    return FALSE;
}

static BOOL menu_icons_enabled(void) {
    DWORD v = 1, size = sizeof(v);
    RegGetValueW(HKEY_CURRENT_USER, L"Software\\ZipNest", L"MenuIcons", RRF_RT_REG_DWORD, NULL, &v, &size);
    return v != 0;
}

/* Selected file-system paths. Caller frees with free_paths. */
typedef struct {
    wchar_t **items;
    DWORD count;
    BOOL all_archives;
    BOOL first_is_dir;
} Selection;

static void free_paths(Selection *s) {
    for (DWORD i = 0; i < s->count; i++) CoTaskMemFree(s->items[i]);
    HeapFree(GetProcessHeap(), 0, s->items);
    s->items = NULL;
    s->count = 0;
}

static BOOL get_selection(IShellItemArray *arr, Selection *s) {
    ZeroMemory(s, sizeof(*s));
    DWORD n = 0;
    if (!arr || FAILED(IShellItemArray_GetCount(arr, &n)) || n == 0) return FALSE;
    s->items = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, n * sizeof(wchar_t *));
    if (!s->items) return FALSE;
    s->all_archives = TRUE;
    for (DWORD i = 0; i < n; i++) {
        IShellItem *it = NULL;
        wchar_t *p = NULL;
        if (FAILED(IShellItemArray_GetItemAt(arr, i, &it))) continue;
        if (SUCCEEDED(IShellItem_GetDisplayName(it, SIGDN_FILESYSPATH, &p)) && p) {
            DWORD attr = GetFileAttributesW(p);
            BOOL dir = attr != INVALID_FILE_ATTRIBUTES && (attr & FILE_ATTRIBUTE_DIRECTORY);
            if (s->count == 0) s->first_is_dir = dir;
            if (dir || !is_archive(p)) s->all_archives = FALSE;
            s->items[s->count++] = p;
        }
        IShellItem_Release(it);
    }
    if (s->count == 0) {
        free_paths(s);
        return FALSE;
    }
    return TRUE;
}

/* "C:\dir\photos.tar.gz" -> "photos" (archive stem) or "report" for a file / "Folder" for a dir. */
static void stem_of(const wchar_t *path, BOOL dir, wchar_t *out, size_t cap) {
    const wchar_t *name = PathFindFileNameW(path);
    wcsncpy(out, name, cap - 1);
    out[cap - 1] = 0;
    if (dir) return;
    size_t len = wcslen(out);
    static const wchar_t *dbl[] = {L".tar.gz", L".tar.bz2", L".tar.xz", L".tar.zst", NULL};
    for (int i = 0; dbl[i]; i++) {
        size_t e = wcslen(dbl[i]);
        if (len > e && _wcsicmp(out + len - e, dbl[i]) == 0) {
            out[len - e] = 0;
            return;
        }
    }
    PathRemoveExtensionW(out);
}

/* Appends one argument with Windows command-line quoting rules. */
static void append_arg(wchar_t *cmd, size_t cap, const wchar_t *arg) {
    size_t n = wcslen(cmd);
    if (n + 4 >= cap) return;
    if (n) cmd[n++] = L' ';
    cmd[n++] = L'"';
    for (const wchar_t *p = arg; *p && n + 4 < cap; p++) {
        size_t backslashes = 0;
        while (*p == L'\\') { backslashes++; p++; }
        if (!*p) { /* trailing backslashes must be doubled before the closing quote */
            for (size_t i = 0; i < backslashes * 2 && n + 2 < cap; i++) cmd[n++] = L'\\';
            break;
        }
        if (*p == L'"') {
            for (size_t i = 0; i < backslashes * 2 + 1 && n + 2 < cap; i++) cmd[n++] = L'\\';
        } else {
            for (size_t i = 0; i < backslashes && n + 2 < cap; i++) cmd[n++] = L'\\';
        }
        cmd[n++] = *p;
    }
    cmd[n++] = L'"';
    cmd[n] = 0;
}

static HRESULT launch(enum Kind kind, Selection *s) {
    wchar_t exe[MAX_PATH];
    exe_path(exe, MAX_PATH);
    size_t cap = 2 * MAX_PATH;
    for (DWORD i = 0; i < s->count; i++) cap += wcslen(s->items[i]) * 2 + 4;
    wchar_t *cmd = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, cap * sizeof(wchar_t));
    if (!cmd) return E_OUTOFMEMORY;
    append_arg(cmd, cap, exe);
    const wchar_t *sw = NULL;
    switch (kind) {
    case K_EXTRACT_DLG: sw = L"--extract-dialog"; break;
    case K_EXTRACT_SUB: sw = L"--extract-auto"; break;
    case K_EXTRACT_HERE: sw = L"--extract-here"; break;
    case K_TEST: sw = L"--test"; break;
    case K_CONVERT: sw = L"--convert"; break;
    case K_ADD: sw = L"--add"; break;
    case K_ADD_ZIP: sw = L"--add-zip"; break;
    default: break;
    }
    if (sw) {
        size_t n = wcslen(cmd);
        wcsncat(cmd, L" ", cap - n - 1);
        wcsncat(cmd, sw, cap - n - 2);
    }
    DWORD count = kind == K_OPEN ? 1 : s->count;
    for (DWORD i = 0; i < count; i++) append_arg(cmd, cap, s->items[i]);

    STARTUPINFOW si;
    ZeroMemory(&si, sizeof(si));
    si.cb = sizeof(si);
    PROCESS_INFORMATION pi;
    BOOL ok = CreateProcessW(exe, cmd, NULL, NULL, FALSE, 0, NULL, NULL, &si, &pi);
    HeapFree(GetProcessHeap(), 0, cmd);
    if (!ok) return HRESULT_FROM_WIN32(GetLastError());
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);
    return S_OK;
}

/* ------------------------------------------------------------------ IExplorerCommand */

typedef struct {
    IExplorerCommand iface;
    LONG ref;
    enum Kind kind;
} Command;

typedef struct {
    IEnumExplorerCommand iface;
    LONG ref;
    ULONG pos;
} Enum;

static IExplorerCommandVtbl command_vtbl;
static IEnumExplorerCommandVtbl enum_vtbl;

static Command *new_command(enum Kind kind) {
    Command *c = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(Command));
    if (!c) return NULL;
    c->iface.lpVtbl = &command_vtbl;
    c->ref = 1;
    c->kind = kind;
    InterlockedIncrement(&g_objects);
    return c;
}

static HRESULT STDMETHODCALLTYPE cmd_QueryInterface(IExplorerCommand *This, REFIID riid, void **ppv) {
    if (!ppv) return E_POINTER;
    if (IsEqualIID(riid, &IID_IUnknown) || IsEqualIID(riid, &IID_IExplorerCommand)) {
        *ppv = This;
        IExplorerCommand_AddRef(This);
        return S_OK;
    }
    *ppv = NULL;
    return E_NOINTERFACE;
}

static ULONG STDMETHODCALLTYPE cmd_AddRef(IExplorerCommand *This) {
    return InterlockedIncrement(&((Command *)This)->ref);
}

static ULONG STDMETHODCALLTYPE cmd_Release(IExplorerCommand *This) {
    Command *c = (Command *)This;
    LONG r = InterlockedDecrement(&c->ref);
    if (r == 0) {
        HeapFree(GetProcessHeap(), 0, c);
        InterlockedDecrement(&g_objects);
    }
    return r;
}

static HRESULT STDMETHODCALLTYPE cmd_GetTitle(IExplorerCommand *This, IShellItemArray *arr, LPWSTR *name) {
    Command *c = (Command *)This;
    wchar_t buf[MAX_PATH + 64];
    Selection s;
    BOOL have = get_selection(arr, &s);
    switch (c->kind) {
    case K_ROOT: wcscpy(buf, L"ZipNest"); break;
    case K_OPEN: wcscpy(buf, L"Open with ZipNest"); break;
    case K_EXTRACT_DLG: wcscpy(buf, L"Extract files..."); break;
    case K_EXTRACT_SUB:
        if (have && s.count == 1) {
            wchar_t stem[MAX_PATH];
            stem_of(s.items[0], FALSE, stem, MAX_PATH);
            _snwprintf(buf, sizeof(buf) / sizeof(buf[0]), L"Extract to \"%ls\\\"", stem);
        } else {
            wcscpy(buf, L"Extract each archive to separate folder");
        }
        break;
    case K_EXTRACT_HERE: wcscpy(buf, L"Extract Here"); break;
    case K_TEST: wcscpy(buf, L"Test archive"); break;
    case K_CONVERT: wcscpy(buf, L"Convert archive..."); break;
    case K_ADD: wcscpy(buf, L"Add to archive..."); break;
    case K_ADD_ZIP: {
        wchar_t stem[MAX_PATH] = L"Archive";
        if (have && s.count == 1) {
            stem_of(s.items[0], s.first_is_dir, stem, MAX_PATH);
        } else if (have) {
            wchar_t dir[MAX_PATH];
            wcsncpy(dir, s.items[0], MAX_PATH - 1);
            dir[MAX_PATH - 1] = 0;
            PathRemoveFileSpecW(dir);
            const wchar_t *d = PathFindFileNameW(dir);
            if (d && *d && d[wcslen(d) - 1] != L'\\') wcsncpy(stem, d, MAX_PATH - 1);
        }
        _snwprintf(buf, sizeof(buf) / sizeof(buf[0]), L"Add to \"%ls.zip\"", stem);
        break;
    }
    default: buf[0] = 0;
    }
    buf[sizeof(buf) / sizeof(buf[0]) - 1] = 0;
    if (have) free_paths(&s);
    return SHStrDupW(buf, name);
}

static HRESULT STDMETHODCALLTYPE cmd_GetIcon(IExplorerCommand *This, IShellItemArray *arr, LPWSTR *icon) {
    (void)arr;
    *icon = NULL;
    if (((Command *)This)->kind != K_ROOT && !menu_icons_enabled()) return E_NOTIMPL;
    wchar_t exe[MAX_PATH + 4];
    exe_path(exe, MAX_PATH);
    wcscat(exe, L",0");
    return SHStrDupW(exe, icon);
}

static HRESULT STDMETHODCALLTYPE cmd_GetToolTip(IExplorerCommand *This, IShellItemArray *arr, LPWSTR *tip) {
    (void)This; (void)arr;
    *tip = NULL;
    return E_NOTIMPL;
}

static HRESULT STDMETHODCALLTYPE cmd_GetCanonicalName(IExplorerCommand *This, GUID *guid) {
    (void)This;
    ZeroMemory(guid, sizeof(*guid));
    return E_NOTIMPL;
}

static HRESULT STDMETHODCALLTYPE cmd_GetState(IExplorerCommand *This, IShellItemArray *arr, BOOL slow, EXPCMDSTATE *state) {
    (void)slow;
    Command *c = (Command *)This;
    Selection s;
    *state = ECS_HIDDEN;
    if (!get_selection(arr, &s)) return S_OK;
    BOOL archives = s.all_archives;
    switch (c->kind) {
    case K_ROOT: *state = ECS_ENABLED; break;
    case K_OPEN: *state = archives && s.count == 1 ? ECS_ENABLED : ECS_HIDDEN; break;
    case K_EXTRACT_DLG:
    case K_EXTRACT_SUB:
    case K_EXTRACT_HERE:
    case K_TEST:
    case K_CONVERT: *state = archives ? ECS_ENABLED : ECS_HIDDEN; break;
    case K_ADD:
    case K_ADD_ZIP: *state = archives ? ECS_HIDDEN : ECS_ENABLED; break;
    default: break;
    }
    free_paths(&s);
    return S_OK;
}

static HRESULT STDMETHODCALLTYPE cmd_Invoke(IExplorerCommand *This, IShellItemArray *arr, IBindCtx *ctx) {
    (void)ctx;
    Command *c = (Command *)This;
    if (c->kind == K_ROOT) return S_OK;
    Selection s;
    if (!get_selection(arr, &s)) return E_INVALIDARG;
    HRESULT hr = launch(c->kind, &s);
    free_paths(&s);
    return hr;
}

static HRESULT STDMETHODCALLTYPE cmd_GetFlags(IExplorerCommand *This, EXPCMDFLAGS *flags) {
    *flags = ((Command *)This)->kind == K_ROOT ? ECF_HASSUBCOMMANDS : ECF_DEFAULT;
    return S_OK;
}

static Enum *new_enum(void) {
    Enum *e = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(Enum));
    if (!e) return NULL;
    e->iface.lpVtbl = &enum_vtbl;
    e->ref = 1;
    InterlockedIncrement(&g_objects);
    return e;
}

static HRESULT STDMETHODCALLTYPE cmd_EnumSubCommands(IExplorerCommand *This, IEnumExplorerCommand **out) {
    *out = NULL;
    if (((Command *)This)->kind != K_ROOT) return E_NOTIMPL;
    Enum *e = new_enum();
    if (!e) return E_OUTOFMEMORY;
    *out = &e->iface;
    return S_OK;
}

static IExplorerCommandVtbl command_vtbl = {
    cmd_QueryInterface, cmd_AddRef, cmd_Release, cmd_GetTitle, cmd_GetIcon, cmd_GetToolTip,
    cmd_GetCanonicalName, cmd_GetState, cmd_Invoke, cmd_GetFlags, cmd_EnumSubCommands,
};

/* ------------------------------------------------------------------ IEnumExplorerCommand */

static HRESULT STDMETHODCALLTYPE enum_QueryInterface(IEnumExplorerCommand *This, REFIID riid, void **ppv) {
    if (!ppv) return E_POINTER;
    if (IsEqualIID(riid, &IID_IUnknown) || IsEqualIID(riid, &IID_IEnumExplorerCommand)) {
        *ppv = This;
        IEnumExplorerCommand_AddRef(This);
        return S_OK;
    }
    *ppv = NULL;
    return E_NOINTERFACE;
}

static ULONG STDMETHODCALLTYPE enum_AddRef(IEnumExplorerCommand *This) {
    return InterlockedIncrement(&((Enum *)This)->ref);
}

static ULONG STDMETHODCALLTYPE enum_Release(IEnumExplorerCommand *This) {
    Enum *e = (Enum *)This;
    LONG r = InterlockedDecrement(&e->ref);
    if (r == 0) {
        HeapFree(GetProcessHeap(), 0, e);
        InterlockedDecrement(&g_objects);
    }
    return r;
}

static HRESULT STDMETHODCALLTYPE enum_Next(IEnumExplorerCommand *This, ULONG celt, IExplorerCommand **out, ULONG *fetched) {
    Enum *e = (Enum *)This;
    ULONG got = 0;
    while (got < celt && e->pos < SUBMENU_LEN) {
        Command *c = new_command(SUBMENU[e->pos++]);
        if (!c) break;
        out[got++] = &c->iface;
    }
    if (fetched) *fetched = got;
    return got == celt ? S_OK : S_FALSE;
}

static HRESULT STDMETHODCALLTYPE enum_Skip(IEnumExplorerCommand *This, ULONG celt) {
    Enum *e = (Enum *)This;
    e->pos = e->pos + celt > SUBMENU_LEN ? SUBMENU_LEN : e->pos + celt;
    return e->pos < SUBMENU_LEN ? S_OK : S_FALSE;
}

static HRESULT STDMETHODCALLTYPE enum_Reset(IEnumExplorerCommand *This) {
    ((Enum *)This)->pos = 0;
    return S_OK;
}

static HRESULT STDMETHODCALLTYPE enum_Clone(IEnumExplorerCommand *This, IEnumExplorerCommand **out) {
    Enum *e = new_enum();
    if (!e) return E_OUTOFMEMORY;
    e->pos = ((Enum *)This)->pos;
    *out = &e->iface;
    return S_OK;
}

static IEnumExplorerCommandVtbl enum_vtbl = {
    enum_QueryInterface, enum_AddRef, enum_Release, enum_Next, enum_Skip, enum_Reset, enum_Clone,
};

/* ------------------------------------------------------------------ class factory */

static HRESULT STDMETHODCALLTYPE cf_QueryInterface(IClassFactory *This, REFIID riid, void **ppv) {
    if (IsEqualIID(riid, &IID_IUnknown) || IsEqualIID(riid, &IID_IClassFactory)) {
        *ppv = This;
        return S_OK;
    }
    *ppv = NULL;
    return E_NOINTERFACE;
}
static ULONG STDMETHODCALLTYPE cf_AddRef(IClassFactory *This) { (void)This; return 2; }
static ULONG STDMETHODCALLTYPE cf_Release(IClassFactory *This) { (void)This; return 1; }

static HRESULT STDMETHODCALLTYPE cf_CreateInstance(IClassFactory *This, IUnknown *outer, REFIID riid, void **ppv) {
    (void)This;
    *ppv = NULL;
    if (outer) return CLASS_E_NOAGGREGATION;
    Command *c = new_command(K_ROOT);
    if (!c) return E_OUTOFMEMORY;
    HRESULT hr = IExplorerCommand_QueryInterface(&c->iface, riid, ppv);
    IExplorerCommand_Release(&c->iface);
    return hr;
}

static HRESULT STDMETHODCALLTYPE cf_LockServer(IClassFactory *This, BOOL lock) {
    (void)This;
    if (lock) InterlockedIncrement(&g_objects);
    else InterlockedDecrement(&g_objects);
    return S_OK;
}

static IClassFactoryVtbl factory_vtbl = {cf_QueryInterface, cf_AddRef, cf_Release, cf_CreateInstance, cf_LockServer};
static IClassFactory factory = {&factory_vtbl};

/* ------------------------------------------------------------------ exports */

STDAPI DllGetClassObject(REFCLSID clsid, REFIID riid, void **ppv) {
    if (!IsEqualCLSID(clsid, &CLSID_ZipNestMenu)) {
        *ppv = NULL;
        return CLASS_E_CLASSNOTAVAILABLE;
    }
    return IClassFactory_QueryInterface(&factory, riid, ppv);
}

STDAPI DllCanUnloadNow(void) {
    return g_objects == 0 ? S_OK : S_FALSE;
}

BOOL WINAPI DllMain(HINSTANCE inst, DWORD reason, LPVOID reserved) {
    (void)reserved;
    if (reason == DLL_PROCESS_ATTACH) {
        g_module = inst;
        DisableThreadLibraryCalls(inst);
    }
    return TRUE;
}
