/*
 * Test harness for ZipNestShell.dll: loads the DLL like Explorer does, builds a
 * selection from the command-line paths and prints the menu it would show.
 * usage: test_shell.exe [--invoke N] path...
 */
#define COBJMACROS
#include <windows.h>
#include <initguid.h>
#include <shobjidl.h>
#include <shlobj.h>
#include <stdio.h>

DEFINE_GUID(CLSID_ZipNestMenu, 0x6f3a2c51, 0x9b7e, 0x4d2a, 0x8c, 0x61, 0x3e, 0x5b, 0x7a, 0x9d, 0x2f, 0x14);

static const char *state_name(EXPCMDSTATE s) { return s & ECS_HIDDEN ? "hidden" : "shown"; }

int wmain(int argc, wchar_t **argv) {
    int invoke = -1, first = 1;
    if (argc > 2 && wcscmp(argv[1], L"--invoke") == 0) {
        invoke = _wtoi(argv[2]);
        first = 3;
    }
    CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
    HMODULE dll = LoadLibraryW(L"ZipNestShell.dll");
    if (!dll) { printf("LoadLibrary failed %lu\n", GetLastError()); return 1; }
    typedef HRESULT (WINAPI *GetClassObject)(REFCLSID, REFIID, void **);
    GetClassObject gco = (GetClassObject)(void *)GetProcAddress(dll, "DllGetClassObject");
    IClassFactory *cf = NULL;
    HRESULT hr = gco(&CLSID_ZipNestMenu, &IID_IClassFactory, (void **)&cf);
    if (FAILED(hr)) { printf("DllGetClassObject 0x%lx\n", hr); return 1; }
    IExplorerCommand *root = NULL;
    hr = IClassFactory_CreateInstance(cf, NULL, &IID_IExplorerCommand, (void **)&root);
    if (FAILED(hr)) { printf("CreateInstance 0x%lx\n", hr); return 1; }

    int n = argc - first;
    PIDLIST_ABSOLUTE *pidls = calloc(n, sizeof(*pidls));
    for (int i = 0; i < n; i++) {
        hr = SHParseDisplayName(argv[first + i], NULL, &pidls[i], 0, NULL);
        if (FAILED(hr)) { wprintf(L"cannot parse %ls 0x%lx\n", argv[first + i], hr); return 1; }
    }
    IShellItemArray *sel = NULL;
    hr = SHCreateShellItemArrayFromIDLists(n, (PCIDLIST_ABSOLUTE_ARRAY)pidls, &sel);
    if (FAILED(hr)) { printf("SHCreateShellItemArrayFromIDLists 0x%lx\n", hr); return 1; }

    LPWSTR s = NULL;
    EXPCMDSTATE st;
    EXPCMDFLAGS fl;
    IExplorerCommand_GetTitle(root, sel, &s);
    IExplorerCommand_GetState(root, sel, TRUE, &st);
    IExplorerCommand_GetFlags(root, &fl);
    wprintf(L"ROOT  \"%ls\" %hs flags=%d\n", s, state_name(st), (int)fl);
    CoTaskMemFree(s);
    IExplorerCommand_GetIcon(root, sel, &s);
    wprintf(L"      icon=%ls\n", s ? s : L"(none)");
    CoTaskMemFree(s);

    IEnumExplorerCommand *e = NULL;
    IExplorerCommand_EnumSubCommands(root, &e);
    IExplorerCommand *c;
    ULONG got;
    int i = 0;
    while (IEnumExplorerCommand_Next(e, 1, &c, &got) == S_OK && got == 1) {
        IExplorerCommand_GetTitle(c, sel, &s);
        IExplorerCommand_GetState(c, sel, TRUE, &st);
        wprintf(L"  [%d] %-45ls %hs\n", i, s, state_name(st));
        CoTaskMemFree(s);
        if (i == invoke) {
            hr = IExplorerCommand_Invoke(c, sel, NULL);
            printf("      -> Invoke 0x%lx\n", hr);
        }
        IExplorerCommand_Release(c);
        i++;
    }
    IEnumExplorerCommand_Release(e);
    IExplorerCommand_Release(root);
    IShellItemArray_Release(sel);
    typedef HRESULT (WINAPI *CanUnload)(void);
    printf("DllCanUnloadNow = %s\n", ((CanUnload)(void *)GetProcAddress(dll, "DllCanUnloadNow"))() == S_OK ? "yes" : "no");
    return 0;
}
