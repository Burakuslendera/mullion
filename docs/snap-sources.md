# Sources for the Snap and Non-Client Region field report

The source list for [snap-and-nonclient-region.md](./snap-and-nonclient-region.md),
moved out of that document verbatim — same numbering, same grouping, same markers.

`[P]` primary (Microsoft Learn, official repos, specs) · `[F]` secondary (issues,
forums, community write-ups).

**WebView2 non-client regions**

1. `[P]` [ICoreWebView2Settings9 — Win32](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2settings9?view=webview2-1.0.2792.45)
2. `[P]` [ICoreWebView2Settings9 — Win32 (later view)](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2settings9?view=webview2-1.0.3296.44)
3. `[P]` [CoreWebView2Settings.IsNonClientRegionSupportEnabled — .NET](https://learn.microsoft.com/en-us/dotnet/api/microsoft.web.webview2.core.corewebview2settings.isnonclientregionsupportenabled?view=webview2-dotnet-1.0.3595.46)
4. `[P]` [CoreWebView2NonClientRegionKind enum](https://learn.microsoft.com/en-us/dotnet/api/microsoft.web.webview2.core.corewebview2nonclientregionkind?view=webview2-dotnet-1.0.2849.39)
5. `[P]` [Overview of WebView2 APIs — draggable / non-client regions](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/overview-features-apis)
6. `[P]` [CoreWebView2WindowControlsOverlay](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/winrt/microsoft_web_webview2_core/corewebview2windowcontrolsoverlay?view=webview2-winrt-1.0.3415-prerelease)
7. `[F]` [WebView2Feedback #200 — implement draggable regions](https://github.com/MicrosoftEdge/WebView2Feedback/issues/200)
8. `[F]` [WebView2Feedback #3562 — custom drag region](https://github.com/MicrosoftEdge/WebView2Feedback/issues/3562)
9. `[F]` [WebView2Feedback #2243 — touch drag window](https://github.com/MicrosoftEdge/WebView2Feedback/issues/2243)
10. `[F]` [WebView2Feedback #649 — move window without a title bar](https://github.com/MicrosoftEdge/WebView2Feedback/issues/649)

**Snap layouts / the `HTMAXBUTTON` requirement**

11. `[P]` [Support snap layouts — Microsoft Learn](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/ui/apply-snap-layout-menu)
12. `[P]` [apply-snap-layout-menu.md — windows-dev-docs source](https://github.com/MicrosoftDocs/windows-dev-docs/blob/docs/hub/apps/desktop/modernize/ui/apply-snap-layout-menu.md)
13. `[F]` [winit #3884 — return HTMAXBUTTON for snap layout](https://github.com/rust-windowing/winit/issues/3884)
14. `[F]` [dotnet/wpf #4825 — snap layout with a custom title bar](https://github.com/dotnet/wpf/issues/4825)
15. `[F]` [dotnet/wpf #8543 — WindowChrome + snap layout](https://github.com/dotnet/wpf/issues/8543)

**The COM ABI (what a hand-written vtable must be derived from)**

16. `[P]` [WebView2 Win32 API reference](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/)
17. `[P]` [Microsoft.Web.WebView2 SDK package — the source of `WebView2.h` / `WebView2.idl`](https://www.nuget.org/packages/Microsoft.Web.WebView2)
18. `[P]` [x64 calling convention (aggregate and floating-point argument passing)](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention)

**DWM caption theming**

19. `[P]` [DWMWA_USE_IMMERSIVE_DARK_MODE — Microsoft Q&A](https://learn.microsoft.com/en-us/answers/questions/966330/dwmwa-use-immersive-dark-mode-confusion)
20. `[P]` [DwmExtendFrameIntoClientArea](https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmextendframeintoclientarea)
21. `[F]` [Dark title bar via immersive dark mode (write-up)](https://www.codestudy.net/blog/winforms-dark-title-bar-on-windows-10/)
22. `[F]` [Windows 11 custom window title colour — forum thread](https://www.purebasic.fr/english/viewtopic.php?t=78732)

**Custom frames done natively (Terminal / Chromium)**

23. `[P]` [Windows Terminal — window features & `NonClientIslandWindow`](https://deepwiki.com/microsoft/terminal/6.2-window-features-and-customization)
24. `[P]` [Chromium — `glass_browser_frame_view.cc` (caption buttons)](https://codereview.chromium.org/2348073002/diff/80001/chrome/browser/ui/views/frame/glass_browser_frame_view.cc)
25. `[P]` [Chromium — top window border with a custom title bar](https://codereview.chromium.org/2381283003/diff/120001/chrome/browser/ui/views/frame/browser_desktop_window_tree_host_win.cc)
26. `[P]` [Chromium — views windowing design doc](https://www.chromium.org/developers/design-documents/views-windowing/)
27. `[F]` [Windhawk — Chromium native title bar (`WM_NCCALCSIZE` / `WM_NCPAINT`)](https://windhawk.net/mods/chromium-native-titlebar)
28. `[F]` [Custom title bar and Windows 10 borders — Handmade Network](https://handmade.network/forums/articles/t/9073-custom_window_title_bar_and_almost_correctly_drawing_windows_10_borders)

**Region holes / `SetWindowRgn`**

29. `[F]` [Using SetWindowRgn](http://www.flounder.com/setwindowrgn.htm)
30. `[F]` [Creating holes in a window — CodeProject](https://www.codeproject.com/Articles/291/Creating-Holes-in-a-Window)

**Electron (the reference architecture for this pattern)**

31. `[P]` [Electron — window customization (`titleBarOverlay` + `app-region`)](https://www.electronjs.org/docs/latest/tutorial/window-customization)
32. `[P]` [Electron PR #29600 — enable Window Controls Overlay](https://github.com/electron/electron/pull/29600)
33. `[P]` [Electron PR #30887 — enable WCO on Windows](https://github.com/electron/electron/pull/30887)
34. `[F]` [Electron #35245 — WCO maximize/snap bug on an external monitor](https://github.com/electron/electron/issues/35245)
35. `[F]` [Frameless Electron with custom controls](https://webtips.dev/seamless-controls-for-your-next-electron-app)

**XAML Islands (the heaviest escape hatch)**

36. `[P]` [Using the WinRT XAML hosting API in a C++ Win32 app](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/xaml-islands/using-the-xaml-hosting-api)
37. `[P]` [Host WinRT XAML controls in desktop apps (XAML Islands)](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/xaml-islands/xaml-islands)
38. `[P]` [Title bar customization (`AppWindow`, Windows 11 Snap)](https://learn.microsoft.com/en-us/windows/apps/develop/title-bar)
39. `[F]` [What is the role of XAML Islands in WinUI 3? — WindowsAppSDK discussion](https://github.com/microsoft/WindowsAppSDK/discussions/465)
40. `[F]` [asklar/xaml-islands + CppXAML helpers](https://github.com/asklar/xaml-islands)
