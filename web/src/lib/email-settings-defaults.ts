import type { OrgEmailTemplate } from '$lib/types';

/** Tokens available in the "Insert placeholder" control (subject & body). */
export const EMAIL_PLACEHOLDER_OPTIONS: string[] = [
	'[Customer Name]',
	'[Customer First Name]',
	'[Trading Name]',
	'[Quote Number]',
	'[Invoice Number]',
	'[Currency Symbol]',
	'[Currency Code]',
	'[Quote Total Without Currency]',
	'[Invoice Total Without Currency]',
	'[Online Quote Link]',
	'[Due Date]',
	'[Statement Date]',
	'[Purchase Order Number]'
];

export const EMAIL_TEMPLATE_TYPE_OPTIONS = [
	'Quote',
	'Remittance',
	'Statement',
	'Credit Note',
	'Purchase Order',
	'Receipt',
	'Sales Invoice',
	'Repeating Invoice'
] as const;

export type EmailTemplateTypeOption = (typeof EMAIL_TEMPLATE_TYPE_OPTIONS)[number];

function row(
	id: string,
	type: string,
	name: string,
	isDefault: boolean,
	subject: string,
	body: string
): OrgEmailTemplate {
	return { ID: id, Type: type, Name: name, IsDefault: isDefault, Subject: subject, Body: body };
}

/** Suggested templates when the organisation has never stored any (Profile.EmailTemplates omitted). */
export function seedEmailTemplates(): OrgEmailTemplate[] {
	return [
		row(
			'seed-quote',
			'Quote',
			'Basic',
			true,
			'Quote [Quote Number] from [Trading Name] for [Customer Name]',
			'Dear [Customer First Name],\n\nThank you for your enquiry. You can view quote [Quote Number] online: [Online Quote Link]\n\nTotal: [Currency Symbol][Quote Total Without Currency] ([Currency Code]).\n\nKind regards,\n[Trading Name]'
		),
		row(
			'seed-remit',
			'Remittance',
			'Basic',
			true,
			'Remittance advice from [Trading Name]',
			'Dear [Customer First Name],\n\nPlease find payment details for your reference.\n\n[Trading Name]'
		),
		row(
			'seed-statement',
			'Statement',
			'Basic',
			true,
			'Account statement from [Trading Name]',
			'Dear [Customer First Name],\n\nPlease find your statement as at [Statement Date].\n\nKind regards,\n[Trading Name]'
		),
		row(
			'seed-cn',
			'Credit Note',
			'Basic',
			true,
			'Credit note [Invoice Number] from [Trading Name]',
			'Dear [Customer First Name],\n\nWe have issued credit note [Invoice Number].\n\nKind regards,\n[Trading Name]'
		),
		row(
			'seed-po',
			'Purchase Order',
			'Basic',
			true,
			'Purchase order [Purchase Order Number] from [Trading Name]',
			'Hello,\n\nPlease see purchase order [Purchase Order Number] for your action.\n\n[Trading Name]'
		),
		row(
			'seed-receipt',
			'Receipt',
			'Basic',
			true,
			'Receipt from [Trading Name]',
			'Dear [Customer First Name],\n\nThank you for your payment.\n\n[Trading Name]'
		),
		row(
			'seed-inv',
			'Sales Invoice',
			'Basic',
			true,
			'Invoice [Invoice Number] from [Trading Name] for [Customer Name]',
			'Dear [Customer First Name],\n\nPlease find invoice [Invoice Number] for [Currency Symbol][Invoice Total Without Currency] ([Currency Code]). Due [Due Date].\n\nKind regards,\n[Trading Name]'
		),
		row(
			'seed-rep',
			'Repeating Invoice',
			'Basic',
			true,
			'Repeating invoice from [Trading Name]',
			'Dear [Customer First Name],\n\nYour repeating invoice is ready. Amount [Currency Symbol][Invoice Total Without Currency] ([Currency Code]).\n\n[Trading Name]'
		),
		row(
			'seed-overdue',
			'Sales Invoice',
			'Overdue — payment reminder',
			false,
			'Reminder: invoice [Invoice Number] from [Trading Name]',
			'Dear [Customer First Name],\n\nOur records show invoice [Invoice Number] for [Currency Symbol][Invoice Total Without Currency] ([Currency Code]) is overdue. Please arrange payment by [Due Date].\n\n[Trading Name]'
		)
	];
}
