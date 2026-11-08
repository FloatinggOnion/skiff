import * as React from 'react'
import { Dialog as DialogPrimitive } from 'radix-ui'

import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { XIcon } from 'lucide-react'

function Dialog({ ...props }: React.ComponentProps<typeof DialogPrimitive.Root>) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />
}

function DialogTrigger({ ...props }: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
}

function DialogPortal({ ...props }: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />
}

function DialogClose({ ...props }: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        'data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0 fixed inset-0 isolate z-50 bg-black/10 duration-100 supports-backdrop-filter:backdrop-blur-xs',
        className,
      )}
      {...props}
    />
  )
}

// Below md a dialog is a drawer from the bottom of the screen, dragged down by its handle to close; from md up it's a centred modal.
function DialogContent({
  className,
  children,
  showCloseButton = true,
  onInteractOutside,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showCloseButton?: boolean
}) {
  const close = React.useRef<HTMLButtonElement>(null)
  const sheet = React.useRef<HTMLDivElement>(null)
  const drag = React.useRef<{ y: number; dy: number } | null>(null)
  const move = (dy: number) => {
    if (sheet.current) sheet.current.style.translate = dy > 0 ? `0 ${dy}px` : ''
  }

  return (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={sheet}
        data-slot="dialog-content"
        className={cn(
          'bg-popover text-popover-foreground ring-foreground/10 data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0 fixed z-50 grid w-full gap-4 overflow-y-auto p-4 text-sm ring-1 outline-none',
          'inset-x-0 bottom-0 max-h-[90dvh] rounded-t-2xl pt-8 pb-[max(1rem,env(safe-area-inset-bottom))] duration-200 data-closed:slide-out-to-bottom data-open:slide-in-from-bottom max-md:max-w-none!',
          'md:inset-x-auto md:top-1/2 md:bottom-auto md:left-1/2 md:max-h-[calc(100dvh-2rem)] sm:max-w-sm md:-translate-x-1/2 md:-translate-y-1/2 md:rounded-xl md:pt-4 md:pb-4 md:duration-100 md:data-closed:zoom-out-95 md:data-closed:[--tw-exit-translate-y:0] md:data-open:zoom-in-95 md:data-open:[--tw-enter-translate-y:0]',
          className,
        )}
        onInteractOutside={(e) => {
          // A confirm prompt renders outside the dialog that opened it, and answering it shouldn't close that dialog.
          const target = e.detail.originalEvent.target as Element | null
          if (target?.closest?.('[data-slot="dialog-content"]')) e.preventDefault()
          onInteractOutside?.(e)
        }}
        {...props}
      >
        <div
          aria-hidden
          className="absolute inset-x-0 top-0 flex h-7 cursor-grab touch-none justify-center pt-2.5 md:hidden"
          onPointerDown={(e) => {
            drag.current = { y: e.clientY, dy: 0 }
            e.currentTarget.setPointerCapture(e.pointerId)
          }}
          onPointerMove={(e) => {
            if (!drag.current) return
            drag.current.dy = e.clientY - drag.current.y
            move(drag.current.dy)
          }}
          onPointerUp={() => {
            const dy = drag.current?.dy ?? 0
            drag.current = null
            if (dy > 80) close.current?.click()
            else move(0)
          }}
          onPointerCancel={() => {
            drag.current = null
            move(0)
          }}
        >
          <span className="bg-foreground/20 h-1 w-10 rounded-full" />
        </div>
        {children}
        <DialogPrimitive.Close ref={close} className="hidden" tabIndex={-1} aria-hidden />
        {showCloseButton && (
          <DialogPrimitive.Close data-slot="dialog-close" asChild>
            <Button variant="ghost" className="absolute top-2 right-2" size="icon-sm">
              <XIcon />
              <span className="sr-only">Close</span>
            </Button>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPortal>
  )
}

function DialogHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div data-slot="dialog-header" className={cn('flex flex-col gap-2', className)} {...props} />
  )
}

function DialogFooter({
  className,
  showCloseButton = false,
  children,
  ...props
}: React.ComponentProps<'div'> & {
  showCloseButton?: boolean
}) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        'bg-muted/50 -mx-4 -mb-[max(1rem,env(safe-area-inset-bottom))] flex flex-col-reverse gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:flex-row sm:justify-end md:-mb-4 md:rounded-b-xl md:pb-4',
        className,
      )}
      {...props}
    >
      {children}
      {showCloseButton && (
        <DialogPrimitive.Close asChild>
          <Button variant="outline">Close</Button>
        </DialogPrimitive.Close>
      )}
    </div>
  )
}

function DialogTitle({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn('font-heading text-base leading-none font-medium', className)}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn(
        'text-muted-foreground *:[a]:hover:text-foreground text-sm *:[a]:underline *:[a]:underline-offset-3',
        className,
      )}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
}
