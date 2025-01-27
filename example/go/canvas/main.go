package main

import (
    "github.com/phillvancejr/webview"
    "fmt"
)

const (
    width = 500
    height = 500
)

func main() {
    w := webview.New()
    defer w.Destroy()
    w.SetSize(width,height, webview.HintNone)
    w.SetTitle("Go Canvas")
    w.Center()
    w.NoCtx()

    content := fmt.Sprintf(`
    <body style="margin:0px;overflow:hidden;">
    <canvas></canvas>
    </body>
    <script>
        const width = %d
        const height = %d
        c = document.querySelector('canvas')
        c.width = width
        c.height = height
        ctx = c.getContext('2d')
        ctx.fillStyle='black'
        ctx.fillRect(0,0,width,height)

        function draw(x,y,w,h) {
            ctx.fillStyle='white'
            ctx.fillRect(x,y,w,h)
        }

        // notify main thread that everything is ready and loaded
        ready()
        // call the backend redSquare function which in tern calls JS
        redSquare()
    </script>
    `, width, height)
    
    // JS calls this backend end function which in tern calls JS directly
    // note here that I don't need w.Dispatch because calls from the JS to the backend are always on the main thread
    w.Bind("redSquare", func() {
        w.Eval("ctx.fillStyle='red';ctx.fillRect(200,300,30,30)")
    })

    // channel for ready notification
    ready := make(chan bool)
    // this function allows JS to notify us when everything is ready and loaded
    w.Bind("ready", func(){
        ready <- true 
    })

    // run in goroutine to avoid blocking 
    go func() {
        // wait for the ready signal
        <-ready
        // w.Dispatch means do something on the main thread
        // you need this when calling JS from goroutines
        w.Dispatch(func(){
            // call the JS draw function
            w.Eval("draw(10,10, 50,50)") 
            w.Eval("ctx.font='100px Arial';ctx.fillText('Hello Go!', 30, 200)")
        })
    }()
    
    w.Navigate("data:text/html,"+content)
    w.Run()
}
