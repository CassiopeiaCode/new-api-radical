import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Form, FormControl, FormDescription, FormField, FormLabel } from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import { SettingsForm, SettingsSwitchContent, SettingsSwitchItem } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type LeakProtectionSectionProps = {
  defaultValues: { LeakProtectionBalancedForceEnabled: boolean }
}

export function LeakProtectionSection({ defaultValues }: LeakProtectionSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm({ defaultValues })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: LeakProtectionSectionProps['defaultValues']) => {
    if (values.LeakProtectionBalancedForceEnabled !== defaultValues.LeakProtectionBalancedForceEnabled) {
      await updateOption.mutateAsync({
        key: 'LeakProtectionBalancedForceEnabled',
        value: values.LeakProtectionBalancedForceEnabled,
      })
    }
  }

  return (
    <SettingsSection title={t('Outbound Credential Protection')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save credential protection'
          />
          <FormField
            control={form.control}
            name='LeakProtectionBalancedForceEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enforce credential protection for every user')}</FormLabel>
                  <FormDescription>
                    {t('When enabled, requests are scanned before relay and users cannot disable this protection in their personal settings. Scanner failures are blocked and logged without request content.')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
