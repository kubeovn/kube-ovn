package vegobserver

import (
	"fmt"
	"net/netip"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

type compiledFilters struct {
	include []compiledRule
	exclude []compiledRule
}

type compiledRule struct {
	addressFamilies map[string]struct{}
	protocols       map[string]struct{}
	natTypes        map[string]struct{}
	original        compiledTuple
	translated      compiledTuple
}

type compiledTuple struct {
	sourceCIDRs, destinationCIDRs []netip.Prefix
	sourcePorts, destinationPorts []apiv1.VpcEgressGatewayPortRange
}

func compileFilters(filters apiv1.VpcEgressGatewayConntrackLogFilters) (compiledFilters, error) {
	compiled := compiledFilters{}
	var err error
	if compiled.include, err = compileRules(filters.Include); err != nil {
		return compiledFilters{}, fmt.Errorf("compile include filters: %w", err)
	}
	if compiled.exclude, err = compileRules(filters.Exclude); err != nil {
		return compiledFilters{}, fmt.Errorf("compile exclude filters: %w", err)
	}
	return compiled, nil
}

func compileRules(rules []apiv1.VpcEgressGatewayConntrackLogFilter) ([]compiledRule, error) {
	result := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		compiled := compiledRule{addressFamilies: stringSet(rule.AddressFamilies), protocols: stringSet(rule.Protocols), natTypes: stringSet(rule.NatTypes)}
		var err error
		if compiled.original, err = compileTuple(rule.Original); err != nil {
			return nil, err
		}
		if compiled.translated, err = compileTuple(rule.Translated); err != nil {
			return nil, err
		}
		result = append(result, compiled)
	}
	return result, nil
}

func compileTuple(tuple apiv1.VpcEgressGatewayConntrackTupleFilter) (compiledTuple, error) {
	result := compiledTuple{sourcePorts: tuple.SourcePorts, destinationPorts: tuple.DestinationPorts}
	for _, raw := range tuple.SourceCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return compiledTuple{}, fmt.Errorf("invalid source CIDR %q: %w", raw, err)
		}
		result.sourceCIDRs = append(result.sourceCIDRs, prefix.Masked())
	}
	for _, raw := range tuple.DestinationCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return compiledTuple{}, fmt.Errorf("invalid destination CIDR %q: %w", raw, err)
		}
		result.destinationCIDRs = append(result.destinationCIDRs, prefix.Masked())
	}
	return result, nil
}

func (filters compiledFilters) match(record flowRecord) bool {
	for _, rule := range filters.exclude {
		if rule.match(record) {
			return false
		}
	}
	if len(filters.include) == 0 {
		return true
	}
	for _, rule := range filters.include {
		if rule.match(record) {
			return true
		}
	}
	return false
}

func (rule compiledRule) match(record flowRecord) bool {
	if !matchesString(rule.addressFamilies, record.AddressFamily) || !matchesString(rule.protocols, record.Protocol) {
		return false
	}
	if len(rule.natTypes) != 0 {
		matched := false
		for _, natType := range record.NatType {
			_, matched = rule.natTypes[natType]
			if matched {
				break
			}
		}
		if _, both := rule.natTypes[metricNatType(record.NatType)]; !matched && !both {
			return false
		}
	}
	return rule.original.match(record.Original) && rule.translated.match(record.Translated)
}

func (tuple compiledTuple) match(value flowTuple) bool {
	if len(tuple.sourceCIDRs) != 0 {
		source, err := netip.ParseAddr(value.SourceIP)
		if err != nil || !matchCIDRs(tuple.sourceCIDRs, source) {
			return false
		}
	}
	if len(tuple.destinationCIDRs) != 0 {
		destination, err := netip.ParseAddr(value.DestinationIP)
		if err != nil || !matchCIDRs(tuple.destinationCIDRs, destination) {
			return false
		}
	}
	return matchPorts(tuple.sourcePorts, value.SourcePort) && matchPorts(tuple.destinationPorts, value.DestinationPort)
}

func matchCIDRs(prefixes []netip.Prefix, address netip.Addr) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func matchPorts(ranges []apiv1.VpcEgressGatewayPortRange, port uint16) bool {
	if len(ranges) == 0 {
		return true
	}
	for _, portRange := range ranges {
		if int32(port) >= portRange.Start && int32(port) <= portRange.End {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func matchesString(values map[string]struct{}, value string) bool {
	if len(values) == 0 {
		return true
	}
	_, ok := values[value]
	return ok
}
