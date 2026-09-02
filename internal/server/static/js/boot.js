(function () {
    var theme = null
    try {
        theme = localStorage.getItem('isane-theme')
    } catch (e) {}
    if (theme !== 'dark' && theme !== 'light') {
        theme = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
    }
    var dark = theme === 'dark'
    document.documentElement.classList.toggle('dark', dark)
    document.querySelector('meta[name="theme-color"]').content = dark ? '#11111b' : '#dce0e8'
    document.querySelector('meta[name="color-scheme"]').content = dark ? 'dark' : 'light'
    var width = null
    try {
        width = parseInt(localStorage.getItem('isane-sidebar-width'), 10)
    } catch (e) {}
    if (!width || width < 200 || width > 400) width = 240
    document.documentElement.style.setProperty('--sidebar-width', width + 'px')
})()
