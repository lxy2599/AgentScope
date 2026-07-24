import { useState, useCallback } from "react";
import { Modal, Select, message, Typography, Collapse, InputNumber, Switch, Space } from "antd";
import { useCreateComparison, useExperiments } from "../../api/hooks";
import { useNavigate } from "react-router-dom";

const { Text } = Typography;

interface Props {
  experimentId: string;
  projectId: string;
  visible: boolean;
  onClose: () => void;
}

interface GatesConfig {
  pass_rate_threshold: number;
  score_delta: number;
  cost_increase_ratio: number;
  p95_latency_increase_pct: number;
  safety_veto: boolean;
}

const DEFAULT_GATES: GatesConfig = {
  pass_rate_threshold: 0.1,
  score_delta: 0.05,
  cost_increase_ratio: 0.5,
  p95_latency_increase_pct: 50,
  safety_veto: true,
};

export function CompareCreateModal({ experimentId, projectId, visible, onClose }: Props) {
  const navigate = useNavigate();
  const [compareType, setCompareType] = useState<string>("general");
  const [candidateId, setCandidateId] = useState<string>("");
  const [gates, setGates] = useState<GatesConfig>({ ...DEFAULT_GATES });
  const createComparison = useCreateComparison(projectId);
  const { data: allExperiments } = useExperiments(projectId);
  const otherExperiments = allExperiments?.filter((e) => e.id !== experimentId) ?? [];

  const resetState = useCallback(() => {
    setCompareType("general");
    setCandidateId("");
    setGates({ ...DEFAULT_GATES });
  }, []);

  const handleSubmit = useCallback(async () => {
    if (!candidateId) {
      message.error("请选择 Candidate 实验");
      return;
    }
    try {
      const result = await createComparison.mutateAsync({
        baseline_experiment_id: experimentId,
        candidate_experiment_id: candidateId,
        comparison_type: compareType,
        regression_gates: JSON.stringify(gates),
      });
      message.success("对比创建成功");
      resetState();
      onClose();
      navigate(`/comparisons/${result.id}`);
    } catch (err: unknown) {
      message.error(err instanceof Error ? err.message : "创建对比失败");
    }
  }, [candidateId, compareType, experimentId, projectId, createComparison, gates, navigate, onClose, resetState]);

  return (
    <Modal
      title="创建实验对比"
      open={visible}
      onCancel={() => { resetState(); onClose(); }}
      onOk={handleSubmit}
      confirmLoading={createComparison.isPending}
      okText="创建对比"
      cancelText="取消"
    >
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8 }}>
          <Text strong>Baseline 实验</Text>
        </div>
        <Text code>{experimentId}</Text>
      </div>
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8 }}>
          <Text strong>对比类型</Text>
        </div>
        <Select
          style={{ width: "100%" }}
          value={compareType}
          onChange={setCompareType}
          options={[
            { value: "general", label: "通用对比" },
            { value: "model_swap", label: "模型替换（对比不同模型时选择）" },
            { value: "ablation", label: "消融实验（对比 Harness 变更时选择）" },
          ]}
        />
      </div>
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8 }}>
          <Text strong>选择 Candidate 实验</Text>
        </div>
        <Select
          style={{ width: "100%" }}
          placeholder="选择 Candidate 实验..."
          value={candidateId}
          onChange={setCandidateId}
          options={otherExperiments.map((e) => ({
            value: e.id,
            label: `${e.target_name} v${e.target_version || "?"}（${e.status === "completed" ? "已完成" : e.status}）`,
          }))}
          notFoundContent="当前项目没有其他实验可对比"
        />
      </div>
      <Collapse
        ghost
        items={[
          {
            key: "gates",
            label: "回归检测阈值配置",
            children: (
              <Space orientation="vertical" style={{ width: "100%" }}>
                <div>
                  <Text>通过率下降阈值</Text>
                  <InputNumber
                    min={0}
                    max={1}
                    step={0.01}
                    value={gates.pass_rate_threshold}
                    onChange={(v) => setGates((prev) => ({ ...prev, pass_rate_threshold: v ?? 0.1 }))}
                    style={{ width: "100%", marginTop: 4 }}
                    suffix="(0~1)"
                  />
                </div>
                <div>
                  <Text>分数下降阈值</Text>
                  <InputNumber
                    min={0}
                    max={1}
                    step={0.01}
                    value={gates.score_delta}
                    onChange={(v) => setGates((prev) => ({ ...prev, score_delta: v ?? 0.05 }))}
                    style={{ width: "100%", marginTop: 4 }}
                    suffix="(0~1)"
                  />
                </div>
                <div>
                  <Text>成本增长比率阈值</Text>
                  <InputNumber
                    min={0}
                    max={10}
                    step={0.1}
                    value={gates.cost_increase_ratio}
                    onChange={(v) => setGates((prev) => ({ ...prev, cost_increase_ratio: v ?? 0.5 }))}
                    style={{ width: "100%", marginTop: 4 }}
                    suffix="(倍数)"
                  />
                </div>
                <div>
                  <Text>时延增长百分比阈值</Text>
                  <InputNumber
                    min={0}
                    max={1000}
                    step={5}
                    value={gates.p95_latency_increase_pct}
                    onChange={(v) => setGates((prev) => ({ ...prev, p95_latency_increase_pct: v ?? 50 }))}
                    style={{ width: "100%", marginTop: 4 }}
                    suffix="(%)"
                  />
                </div>
                <div>
                  <Space>
                    <Text>安全性否决</Text>
                    <Switch
                      checked={gates.safety_veto}
                      onChange={(v) => setGates((prev) => ({ ...prev, safety_veto: v }))}
                    />
                  </Space>
                </div>
              </Space>
            ),
          },
        ]}
      />
      <div style={{ marginTop: 8 }}>
        <Text type="secondary">
          回归检测将根据所选阈值判断候选实验是否产生回归。对比创建后将自动跳转到对比报告页。
        </Text>
      </div>
    </Modal>
  );
}
