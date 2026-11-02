import * as React from 'react'
import { cn } from '@/lib/utils'
import { Label } from '@/components/ui/label'

function FieldSet({ className, ...props }: React.ComponentProps<'fieldset'>) {
  return (
    <fieldset data-slot="field-set" className={cn('flex flex-col gap-6', className)} {...props} />
  )
}

function FieldLegend({
  className,
  variant = 'legend',
  ...props
}: React.ComponentProps<'legend'> & { variant?: 'legend' | 'label' }) {
  return (
    <legend
      data-slot="field-legend"
      data-variant={variant}
      className={cn(
        'mb-3 font-medium',
        variant === 'legend' && 'text-base',
        variant === 'label' && 'text-sm',
        className,
      )}
      {...props}
    />
  )
}

function FieldGroup({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="field-group"
      className={cn('flex w-full flex-col gap-5', className)}
      {...props}
    />
  )
}

function Field({
  className,
  orientation = 'vertical',
  ...props
}: React.ComponentProps<'div'> & { orientation?: 'vertical' | 'horizontal' | 'responsive' }) {
  return (
    <div
      role="group"
      data-slot="field"
      data-orientation={orientation}
      className={cn(
        'group/field data-[invalid=true]:text-destructive flex w-full gap-2',
        'data-[orientation=vertical]:flex-col',
        'data-[orientation=horizontal]:flex-row data-[orientation=horizontal]:items-center',
        className,
      )}
      {...props}
    />
  )
}

function FieldContent({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="field-content"
      className={cn('flex flex-1 flex-col gap-1.5', className)}
      {...props}
    />
  )
}

function FieldLabel({ className, ...props }: React.ComponentProps<typeof Label>) {
  return (
    <Label
      data-slot="field-label"
      className={cn('text-muted-foreground gap-1 text-xs font-normal', className)}
      {...props}
    />
  )
}

function FieldTitle({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="field-title" className={cn('text-sm font-medium', className)} {...props} />
}

function FieldDescription({ className, ...props }: React.ComponentProps<'p'>) {
  return (
    <p
      data-slot="field-description"
      className={cn('text-muted-foreground text-xs', className)}
      {...props}
    />
  )
}

type FieldErrorProps = React.ComponentProps<'p'> & {
  errors?: Array<{ message?: string } | undefined>
}

function FieldError({ className, children, errors, ...props }: FieldErrorProps) {
  const messages = errors?.map((e) => e?.message).filter(Boolean) as string[] | undefined
  const content =
    children ??
    (!messages || messages.length === 0 ? null : messages.length === 1 ? (
      messages[0]
    ) : (
      <ul className="list-disc space-y-0.5 pl-4">
        {messages.map((m, i) => (
          <li key={i}>{m}</li>
        ))}
      </ul>
    ))
  if (!content) return null
  return (
    <p
      role="alert"
      data-slot="field-error"
      className={cn('text-xs text-danger', className)}
      {...props}
    >
      {content}
    </p>
  )
}

function FieldSeparator({ className, children, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="field-separator"
      className={cn('relative my-1 text-center', className)}
      {...props}
    >
      <span className="bg-border absolute inset-x-0 top-1/2 h-px" />
      {children && (
        <span className="bg-background text-muted-foreground relative px-2 text-xs">
          {children}
        </span>
      )}
    </div>
  )
}

export {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldTitle,
}
