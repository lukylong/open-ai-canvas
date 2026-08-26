import { App, Button, DatePicker, Drawer, Form, Input, InputNumber, Space, Table, Tag, Typography } from "antd";
import { Copy, Plus, RotateCcw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import {
    createAdminInvitationCode,
    listAdminInvitationCodes,
    revokeAdminInvitationCode,
    type InvitationCode,
} from "@/services/api/auth";
import { AdminPageFrame } from "../components/admin-shell";

type CreateInviteForm = { label?: string; maxUses: number; expiresAt?: { toISOString(): string } };

export default function InvitationCodesPage() {
    const { message, modal } = App.useApp();
    const [items, setItems] = useState<InvitationCode[]>([]);
    const [loading, setLoading] = useState(true);
    const [open, setOpen] = useState(false);
    const [creating, setCreating] = useState(false);
    const [createdCode, setCreatedCode] = useState("");
    const [form] = Form.useForm<CreateInviteForm>();

    const reload = useCallback(async () => {
        setLoading(true);
        try {
            const result = await listAdminInvitationCodes();
            setItems(result.invitationCodes);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取邀请码失败");
        } finally {
            setLoading(false);
        }
    }, [message]);

    useEffect(() => { void reload(); }, [reload]);

    const create = async () => {
        const values = await form.validateFields();
        setCreating(true);
        try {
            const result = await createAdminInvitationCode({
                label: values.label?.trim(),
                maxUses: values.maxUses,
                expiresAt: values.expiresAt?.toISOString(),
            });
            setCreatedCode(result.invitationCode.code);
            await reload();
            message.success("邀请码已创建，请立即复制保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建邀请码失败");
        } finally {
            setCreating(false);
        }
    };

    const revoke = (invite: InvitationCode) => modal.confirm({
        title: "撤销邀请码？",
        content: `${invite.codePreview} 撤销后不能继续注册。`,
        okText: "撤销",
        okButtonProps: { danger: true },
        cancelText: "取消",
        onOk: async () => {
            await revokeAdminInvitationCode(invite.id);
            await reload();
            message.success("邀请码已撤销");
        },
    });

    return (
        <AdminPageFrame
            title="邀请码"
            description="新账号必须使用有效邀请码，邮箱为选填项"
            scroll
            actions={<Space><Button icon={<RotateCcw className="size-4" />} onClick={() => void reload()}>刷新</Button><Button type="primary" icon={<Plus className="size-4" />} onClick={() => { setCreatedCode(""); form.resetFields(); form.setFieldValue("maxUses", 1); setOpen(true); }}>创建邀请码</Button></Space>}
        >
            <Table<InvitationCode>
                className="mt-4"
                rowKey="id"
                loading={loading}
                dataSource={items}
                pagination={{ pageSize: 20 }}
                columns={[
                    { title: "邀请码", dataIndex: "codePreview", width: 150 },
                    { title: "备注", dataIndex: "label", render: (value: string) => value || "--" },
                    { title: "使用", width: 120, render: (_, item) => `${item.usedCount}/${item.maxUses || "不限"}` },
                    { title: "有效期", width: 190, render: (_, item) => item.expiresAt ? new Date(item.expiresAt).toLocaleString("zh-CN", { hour12: false }) : "长期" },
                    { title: "状态", width: 110, render: (_, item) => <InviteStatus invite={item} /> },
                    { title: "操作", width: 100, render: (_, item) => <Button danger type="link" disabled={Boolean(item.revokedAt)} onClick={() => revoke(item)}>撤销</Button> },
                ]}
            />
            <Drawer title="创建邀请码" width={420} open={open} onClose={() => setOpen(false)} extra={<Button type="primary" loading={creating} disabled={Boolean(createdCode)} onClick={() => void create()}>创建</Button>}>
                {createdCode ? (
                    <div className="space-y-3 rounded-lg border border-border bg-surface-subtle p-4">
                        <Typography.Text type="secondary">邀请码只在本次创建后展示完整值：</Typography.Text>
                        <Typography.Title level={4} copyable={{ text: createdCode, icon: <Copy className="size-4" /> }}>{createdCode}</Typography.Title>
                        <Button block onClick={() => { setOpen(false); setCreatedCode(""); }}>完成</Button>
                    </div>
                ) : (
                    <Form form={form} layout="vertical" initialValues={{ maxUses: 1 }}>
                        <Form.Item name="label" label="备注"><Input maxLength={60} placeholder="例如：运营部 8 月邀请" /></Form.Item>
                        <Form.Item name="maxUses" label="最多使用次数" extra="0 表示不限次数" rules={[{ required: true }, { type: "number", min: 0, max: 10000 }]}><InputNumber className="w-full" min={0} max={10000} precision={0} /></Form.Item>
                        <Form.Item name="expiresAt" label="过期时间（选填）"><DatePicker className="w-full" showTime /></Form.Item>
                    </Form>
                )}
            </Drawer>
        </AdminPageFrame>
    );
}

function InviteStatus({ invite }: { invite: InvitationCode }) {
    if (invite.revokedAt) return <Tag color="red">已撤销</Tag>;
    if (invite.expiresAt && new Date(invite.expiresAt).getTime() <= Date.now()) return <Tag>已过期</Tag>;
    if (invite.maxUses > 0 && invite.usedCount >= invite.maxUses) return <Tag>已用完</Tag>;
    return <Tag color="green">有效</Tag>;
}
