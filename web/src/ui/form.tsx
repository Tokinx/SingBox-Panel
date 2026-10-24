import React, {
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
} from 'react'
import {
  useForm as useHookForm,
  useController,
  useWatch,
  useFieldArray,
  type UseFormReturn,
} from 'react-hook-form'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

type Values = Record<string, any>
type Name = string | number | (string | number)[]
export type FormInstance<T extends Values = any> = {
  getFieldValue(name: Name): any
  getFieldsValue(): T
  setFieldValue(name: Name, value: any): void
  setFieldsValue(values: Partial<T>): void
  resetFields(): void
  validateFields(): Promise<T>
  scrollToField(name: Name, options?: ScrollIntoViewOptions): void
  _raw: UseFormReturn<Values>
  _initial: React.MutableRefObject<Values>
  _id: string
  _names: Set<string>
}
const FormContext = createContext<FormInstance | null>(null)
const ListContext = createContext<string>('')
const path = (name: Name) =>
  Array.isArray(name) ? name.join('.') : String(name)

function useForm<T extends Values = any>(): [FormInstance<T>] {
  const raw = useHookForm<Values>({
    mode: 'onSubmit',
    reValidateMode: 'onChange',
    shouldUnregister: false,
  })
  const initial = useRef<Values>({})
  const id = useId()
  const names = useRef(new Set<string>())
  const instance = useRef<FormInstance<T>>()
  if (!instance.current)
    instance.current = {
      _raw: raw,
      _initial: initial,
      _id: id,
      _names: names.current,
      getFieldValue: (name) => raw.getValues(path(name)),
      getFieldsValue: () => raw.getValues() as T,
      setFieldValue: (name, value) => raw.setValue(path(name), value),
      setFieldsValue: (values) =>
        Object.entries(values).forEach(([key, value]) =>
          raw.setValue(key, value),
        ),
      resetFields: () => raw.reset(initial.current),
      validateFields: async () => {
        if (!(await raw.trigger([...names.current]))) {
          const errorFields = [...names.current].flatMap((name) => {
            const error = raw.getFieldState(name).error
            return error
              ? [
                  {
                    name: name.split('.'),
                    errors: [error.message || '输入不正确'],
                  },
                ]
              : []
          })
          throw { errorFields, values: raw.getValues() }
        }
        return raw.getValues() as T
      },
      scrollToField: (name, options) => {
        const field = document.getElementById(`${id}-${path(name)}`)
        field?.closest('details')?.setAttribute('open', '')
        field?.scrollIntoView(options)
        field?.focus({ preventScroll: true })
      },
    }
  // Subscribe to errors for validateFields' recursive extraction.
  void raw.formState.errors
  return [instance.current]
}

type RuleObject = {
  required?: boolean
  whitespace?: boolean
  message?: string
  min?: number
  max?: number
  len?: number
  pattern?: RegExp
  type?: string
  validator?: (rule: RuleObject, value: any) => Promise<void> | void
}
type Rule = RuleObject | ((form: FormInstance) => RuleObject)
type ItemProps = {
  name?: Name
  label?: React.ReactNode
  children?: React.ReactNode
  rules?: Rule[]
  className?: string
  style?: React.CSSProperties
  noStyle?: boolean
  required?: boolean
  extra?: React.ReactNode
  help?: React.ReactNode
  valuePropName?: string
  getValueFromEvent?: (...args: any[]) => any
  getValueProps?: (value: any) => Values
  dependencies?: Name[]
  initialValue?: any
  hidden?: boolean
  fieldKey?: number
}
function FormItem(props: ItemProps) {
  if (props.name !== undefined) return <BoundItem {...props} />
  return <ItemLayout {...props}>{props.children}</ItemLayout>
}
function ItemLayout({
  label,
  children,
  className,
  style,
  noStyle,
  required,
  extra,
  help,
  hidden,
  id,
  error,
}: ItemProps & { id?: string; error?: string }) {
  if (noStyle)
    return (
      <>
        {children}
        {error && (
          <span className="ui-form-item-explain" role="alert">
            {error}
          </span>
        )}
      </>
    )
  return (
    <div
      className={cn(
        'ui-form-item',
        error && 'ui-form-item-has-error',
        className,
      )}
      style={style}
      hidden={hidden}
    >
      <div className="ui-form-item-row">
        {label && (
          <div className="ui-form-item-label">
            <Label htmlFor={id}>
              {label}
              {required && <span className="ui-required">*</span>}
            </Label>
          </div>
        )}
        <div className="ui-form-item-control">
          {children}
          {(error || help) && (
            <div
              id={id ? `${id}-error` : undefined}
              className="ui-form-item-explain"
              role={error ? 'alert' : undefined}
            >
              {error || help}
            </div>
          )}
          {extra && <div className="ui-form-item-extra">{extra}</div>}
        </div>
      </div>
    </div>
  )
}
function BoundItem(props: ItemProps) {
  const form = useContext(FormContext)!
  const prefix = useContext(ListContext)
  const name = [prefix, path(props.name!)].filter(Boolean).join('.')
  const rules = (props.rules || []).map((rule) =>
    typeof rule === 'function' ? rule(form) : rule,
  )
  const { field, fieldState } = useController({
    name,
    control: form._raw.control,
    defaultValue: props.initialValue,
    rules: {
      validate: async (value) => {
        for (const rule of rules) {
          const empty =
            value === undefined ||
            value === null ||
            value === '' ||
            (Array.isArray(value) && !value.length) ||
            (rule.whitespace && typeof value === 'string' && !value.trim())
          if (rule.required && empty)
            return (
              rule.message ||
              `请填写${typeof props.label === 'string' ? props.label : '此项'}`
            )
          if (!empty) {
            const size = typeof value === 'number' ? value : value.length
            if (
              (rule.min !== undefined && size < rule.min) ||
              (rule.max !== undefined && size > rule.max) ||
              (rule.len !== undefined && size !== rule.len)
            )
              return rule.message || '输入长度或数值不符合要求'
            if (rule.pattern && !rule.pattern.test(value))
              return rule.message || '输入格式不正确'
            if (
              rule.type === 'email' &&
              !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)
            )
              return rule.message || '邮箱格式不正确'
          }
          try {
            await rule.validator?.(rule, value)
          } catch (error) {
            return error instanceof Error
              ? error.message
              : rule.message || '输入格式不正确'
          }
        }
        return true
      },
    },
  })
  useEffect(() => {
    form._names.add(name)
    return () => {
      form._names.delete(name)
    }
  }, [form, name])
  const id = `${form._id}-${name}`
  const child = React.Children.only(props.children) as React.ReactElement<any>
  const valueProps = props.getValueProps
    ? props.getValueProps(field.value)
    : {
        [props.valuePropName || 'value']:
          field.value ?? (props.valuePropName === 'checked' ? false : ''),
      }
  const control = React.cloneElement(child, {
    ...valueProps,
    id,
    ref: field.ref,
    'aria-invalid': fieldState.invalid || undefined,
    'aria-describedby': fieldState.error ? `${id}-error` : undefined,
    onBlur: (...args: any[]) => {
      field.onBlur()
      child.props.onBlur?.(...args)
    },
    onChange: (...args: any[]) => {
      const first = args[0]
      const value = props.getValueFromEvent
        ? props.getValueFromEvent(...args)
        : first?.target
          ? first.target[
              props.valuePropName === 'checked' ? 'checked' : 'value'
            ]
          : first
      field.onChange(value)
      child.props.onChange?.(...args)
    },
  })
  return (
    <ItemLayout
      {...props}
      id={id}
      error={fieldState.error?.message}
      required={props.required || rules.some((rule) => rule.required)}
    >
      {control}
    </ItemLayout>
  )
}
function FormRoot({
  form: provided,
  children,
  initialValues,
  onFinish,
  onFinishFailed,
  className,
  layout = 'vertical',
  requiredMark,
  name,
  ...rest
}: Omit<React.FormHTMLAttributes<HTMLFormElement>, 'onFinish'> & {
  form?: FormInstance
  initialValues?: Values
  onFinish?: (values: any) => void | Promise<void>
  onFinishFailed?: (error: any) => void
  layout?: string
  requiredMark?: boolean
  name?: string
}) {
  const [fallback] = useForm()
  const form = provided || fallback
  const initialized = useRef(false)
  useEffect(() => {
    if (initialized.current) return
    initialized.current = true
    form._initial.current = initialValues || {}
    for (const [key, value] of Object.entries(initialValues || {}))
      if (form.getFieldValue(key) === undefined) form.setFieldValue(key, value)
  }, [form, initialValues])
  return (
    <FormContext.Provider value={form}>
      <form
        {...rest}
        name={name}
        className={cn('ui-form', `ui-form-${layout}`, className)}
        onSubmit={async (event) => {
          event.preventDefault()
          try {
            const values = await form.validateFields()
            await onFinish?.(values)
          } catch (error: any) {
            onFinishFailed?.(error)
            if (error?.errorFields?.[0])
              form.scrollToField(error.errorFields[0].name, { block: 'center' })
          }
        }}
      >
        {children}
      </form>
    </FormContext.Provider>
  )
}
function FormList({
  name,
  children,
}: {
  name: string
  children: (
    fields: { key: string; name: number }[],
    operations: {
      add: (value: any) => void
      remove: (index: number) => void
      move: (from: number, to: number) => void
    },
  ) => React.ReactNode
}) {
  const form = useContext(FormContext)!
  const list = useFieldArray({ name, control: form._raw.control })
  return (
    <ListContext.Provider value={name}>
      {children(
        list.fields.map((field, index) => ({ key: field.id, name: index })),
        {
          add: (value) => list.append(value),
          remove: list.remove,
          move: list.move,
        },
      )}
    </ListContext.Provider>
  )
}
function watch(name: Name, form: FormInstance) {
  return useWatch({ name: path(name), control: form._raw.control })
}
export const Form = Object.assign(FormRoot, {
  Item: FormItem,
  List: FormList,
  useForm,
  useWatch: watch,
})
